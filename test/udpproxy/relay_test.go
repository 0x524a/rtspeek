package main

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func udpSock(t *testing.T) *net.UDPConn {
	t.Helper()
	c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func addrOf(c *net.UDPConn) *net.UDPAddr { return c.LocalAddr().(*net.UDPAddr) }

// recvAll reads packets from c until nothing arrives for quiet.
func recvAll(c *net.UDPConn, quiet time.Duration) [][]byte {
	var out [][]byte
	buf := make([]byte, 2048)
	for {
		_ = c.SetReadDeadline(time.Now().Add(quiet))
		n, _, err := c.ReadFromUDP(buf)
		if err != nil {
			return out
		}
		out = append(out, append([]byte(nil), buf[:n]...))
	}
}

func rtpPacket(seq byte) []byte {
	p := make([]byte, 14)
	p[0] = 0x80
	p[1] = 96
	p[13] = seq
	return p
}

func TestRelayForwardsDropsAndKeepsOrder(t *testing.T) {
	for _, tc := range []struct {
		name string
		im   *impairment
		want int
	}{
		{"no impairment", nil, 20},
		{"zero loss", newImpairment(0, 0, 0, 0, 0, false, 0, 1), 20},
		{"total loss", newImpairment(1, 0, 0, 0, 0, false, 0, 1), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, out, dst := udpSock(t), udpSock(t), udpSock(t)
			go relay(in, out, addrOf(dst), tc.im)
			src := udpSock(t)
			for i := range 20 {
				if _, err := src.WriteToUDP(rtpPacket(byte(i)), addrOf(in)); err != nil {
					t.Fatal(err)
				}
			}
			got := recvAll(dst, 300*time.Millisecond)
			if len(got) != tc.want {
				t.Fatalf("received %d packets, want %d", len(got), tc.want)
			}
			for i, p := range got {
				if p[13] != byte(i) {
					t.Fatalf("packet %d carries sequence %d: order changed", i, p[13])
				}
			}
		})
	}
}

func TestRelayDelaysPacketsInOrder(t *testing.T) {
	in, out, dst := udpSock(t), udpSock(t), udpSock(t)
	im := newImpairment(0, 80*time.Millisecond, 160*time.Millisecond, 0, 0, false, 0, 3)
	go relay(in, out, addrOf(dst), im)
	src := udpSock(t)
	start := time.Now()
	for i := range 10 {
		_, _ = src.WriteToUDP(rtpPacket(byte(i)), addrOf(in))
	}
	got := recvAll(dst, 500*time.Millisecond)
	if len(got) != 10 {
		t.Fatalf("received %d packets, want 10", len(got))
	}
	for i, p := range got {
		if p[13] != byte(i) {
			t.Fatalf("ordered delay changed the order at %d", i)
		}
	}
	if time.Since(start) < 80*time.Millisecond {
		t.Fatalf("packets were not held back")
	}
}

func TestRelayReorderModeStillDeliversEverything(t *testing.T) {
	in, out, dst := udpSock(t), udpSock(t), udpSock(t)
	im := newImpairment(0, 0, 150*time.Millisecond, 0, 0, true, 0, 5)
	go relay(in, out, addrOf(dst), im)
	src := udpSock(t)
	for i := range 30 {
		_, _ = src.WriteToUDP(rtpPacket(byte(i)), addrOf(in))
	}
	got := recvAll(dst, 400*time.Millisecond)
	if len(got) != 30 {
		t.Fatalf("received %d packets, want 30", len(got))
	}
	inOrder := true
	for i, p := range got {
		if p[13] != byte(i) {
			inOrder = false
		}
	}
	if inOrder {
		t.Fatal("reorder mode delivered everything in order")
	}
}

// tcpPair returns both ends of a loopback TCP connection.
func tcpPair(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	ch := make(chan net.Conn, 1)
	go func() { c, _ := ln.Accept(); ch <- c }()
	a, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	b := <-ch
	t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
	return a, b
}

func TestRequestAndResponseRewriteSetupPorts(t *testing.T) {
	_, srvSide := tcpPair(t) // RemoteAddr is a loopback client
	p := &proxyConn{client: srvSide, upstreamAddr: "127.0.0.1:8554", listenAddr: "127.0.0.1:8555", im: nil,
		methods: map[string]string{}, pending: map[string]*udpSession{}}
	t.Cleanup(p.closeSessions)

	req := &message{start: "SETUP rtsp://127.0.0.1:8555/s/trackID=0 RTSP/1.0",
		headers: []string{"CSeq: 4", "Transport: RTP/AVP;unicast;client_port=5000-5001"}}
	if err := p.request(req); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(req.start, "127.0.0.1:8554") {
		t.Fatalf("URL not pointed at the upstream: %q", req.start)
	}
	up, _, _ := portPair(clientPortRe, req.header("Transport"))
	if up == 5000 || up%2 != 0 {
		t.Fatalf("upstream client_port = %d, want the proxy's own even port", up)
	}

	resp := &message{start: "RTSP/1.0 200 OK",
		headers: []string{"CSeq: 4", "Transport: RTP/AVP;unicast;client_port=" + fmt.Sprint(up) + "-" + fmt.Sprint(up+1) + ";server_port=9000-9001"}}
	if err := p.response(resp); err != nil {
		t.Fatal(err)
	}
	down, _, _ := portPair(serverPortRe, resp.header("Transport"))
	if down == 9000 || down%2 != 0 {
		t.Fatalf("client-facing server_port = %d, want the proxy's own even port", down)
	}

	bad := &message{start: "RTSP/1.0 200 OK", headers: []string{"CSeq: 5", "Transport: RTP/AVP;unicast"}}
	p.pending["5"] = &udpSession{}
	if err := p.response(bad); err == nil {
		t.Fatal("a SETUP response without server_port must be an error")
	}
}

func TestRequestLeavesTCPInterleavedSetupAlone(t *testing.T) {
	_, srvSide := tcpPair(t)
	p := &proxyConn{client: srvSide, upstreamAddr: "u:1", listenAddr: "l:1", methods: map[string]string{}, pending: map[string]*udpSession{}}
	tr := "RTP/AVP/TCP;unicast;interleaved=0-1"
	req := &message{start: "SETUP rtsp://l:1/s RTSP/1.0", headers: []string{"CSeq: 2", "Transport: " + tr}}
	if err := p.request(req); err != nil || req.header("Transport") != tr || len(p.pending) != 0 {
		t.Fatalf("interleaved SETUP was touched: %v %q %v", err, req.header("Transport"), p.pending)
	}
}

// fakeServer is a minimal RTSP server: it answers SETUP with its own UDP ports,
// and on PLAY sends n RTP packets to the client port it was given.
type fakeServer struct {
	ln       net.Listener
	rtp      *net.UDPConn
	rtcp     *net.UDPConn
	gotPunch chan []byte
}

func newFakeServer(t *testing.T, n int, idr bool) *fakeServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rtp, rtcp, err := listenPair()
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeServer{ln: ln, rtp: rtp, rtcp: rtcp, gotPunch: make(chan []byte, 4)}
	t.Cleanup(func() { _ = ln.Close(); _ = rtp.Close(); _ = rtcp.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			k, _, err := rtp.ReadFromUDP(buf)
			if err != nil {
				return
			}
			s.gotPunch <- append([]byte(nil), buf[:k]...)
		}
	}()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		br := bufio.NewReader(c)
		var client *net.UDPAddr
		for {
			m, err := readMessage(br)
			if err != nil {
				return
			}
			cseq := m.header("CSeq")
			reply := "RTSP/1.0 200 OK\r\nCSeq: " + cseq + "\r\n"
			switch m.method() {
			case "SETUP":
				a, _, _ := portPair(clientPortRe, m.header("Transport"))
				client = &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: a}
				sp := addrOf(rtp).Port
				reply += fmt.Sprintf("Transport: RTP/AVP;unicast;client_port=%d-%d;server_port=%d-%d\r\nSession: abc\r\n", a, a+1, sp, sp+1)
			case "PLAY":
				go func() {
					for i := range n {
						p := rtpPacket(byte(i))
						if idr { // one IDR frame as 4 FU-A fragments
							p = append(p[:12:12], 0x7c, []byte{0x85, 0x05, 0x05, 0x45}[i%4])
						}
						_, _ = rtp.WriteToUDP(p, client)
						time.Sleep(time.Millisecond)
					}
				}()
			}
			_, _ = c.Write([]byte(reply + "\r\n"))
		}
	}()
	return s
}

// startProxy runs the proxy in front of s and returns its address.
func startProxy(t *testing.T, s *fakeServer, im *impairment) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go (&proxyConn{client: c, upstreamAddr: s.ln.Addr().String(), listenAddr: ln.Addr().String(), im: im}).run()
		}
	}()
	return ln.Addr().String()
}

// play drives SETUP and PLAY through the proxy and returns the client's RTP
// socket plus the server port the proxy advertised.
func play(t *testing.T, proxy string) (*net.UDPConn, int) {
	t.Helper()
	c, err := net.Dial("tcp", proxy)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	rtp, rtcp, err := listenPair()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rtp.Close(); _ = rtcp.Close() })
	br := bufio.NewReader(c)
	send := func(req string) *message {
		if _, err := c.Write([]byte(req)); err != nil {
			t.Fatal(err)
		}
		_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
		m, err := readMessage(br)
		if err != nil || !strings.Contains(m.start, "200") {
			t.Fatalf("response %+v, err %v", m, err)
		}
		return m
	}
	cp := addrOf(rtp).Port
	m := send(fmt.Sprintf("SETUP rtsp://%s/s/trackID=0 RTSP/1.0\r\nCSeq: 1\r\nTransport: RTP/AVP;unicast;client_port=%d-%d\r\n\r\n", proxy, cp, cp+1))
	sp, _, ok := portPair(serverPortRe, m.header("Transport"))
	if !ok {
		t.Fatalf("no server_port in %q", m.header("Transport"))
	}
	send(fmt.Sprintf("PLAY rtsp://%s/s RTSP/1.0\r\nCSeq: 2\r\nSession: abc\r\n\r\n", proxy))
	return rtp, sp
}

func TestProxyEndToEndPassthroughAndLoss(t *testing.T) {
	for _, tc := range []struct {
		name string
		im   *impairment
		want int
	}{
		{"passthrough", newImpairment(0, 0, 0, 0, 0, false, 0, 1), 30},
		{"total loss", newImpairment(1, 0, 0, 0, 0, false, 0, 1), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newFakeServer(t, 30, false)
			rtp, sp := play(t, startProxy(t, s, tc.im))
			if sp == addrOf(s.rtp).Port {
				t.Fatal("the proxy must advertise its own server port, not the server's")
			}
			got := recvAll(rtp, 400*time.Millisecond)
			if len(got) != tc.want {
				t.Fatalf("client received %d packets, want %d", len(got), tc.want)
			}
		})
	}
}

func TestProxyEndToEndForwardsClientPacketsToServer(t *testing.T) {
	s := newFakeServer(t, 1, false)
	rtp, sp := play(t, startProxy(t, s, newImpairment(0, 0, 0, 0, 0, false, 0, 1)))
	_ = recvAll(rtp, 200*time.Millisecond)
	if _, err := rtp.WriteToUDP([]byte("punch"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: sp}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-s.gotPunch:
		if string(got) != "punch" {
			t.Fatalf("server got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the server never received the client's packet")
	}
}

func TestProxyEndToEndDropsOneKeyframeFragment(t *testing.T) {
	s := newFakeServer(t, 4, true)
	rtp, _ := play(t, startProxy(t, s, newImpairment(0, 0, 0, 0, 0, false, 1, 1)))
	got := recvAll(rtp, 400*time.Millisecond)
	if len(got) != 3 {
		t.Fatalf("client received %d fragments of the keyframe, want 3 (one dropped)", len(got))
	}
}

func TestProxyClosesSessionsWhenClientLeaves(t *testing.T) {
	s := newFakeServer(t, 0, false)
	addr := startProxy(t, s, nil)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		c, err := net.Dial("tcp", addr)
		if err != nil {
			return
		}
		_ = c.Close()
	}()
	wg.Wait() // must not hang or panic when the client disappears before SETUP
}

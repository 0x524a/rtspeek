// Command udpproxy sits between an RTSP client and server and impairs the UDP
// media path, for testing rtspeek --analyze: it can drop RTP packets, hold them
// back for a random time, let them overtake each other, and drop one fragment
// of a chosen H.264 keyframe.
//
// RTSP control and TCP-interleaved media pass through untouched. Only UDP
// unicast RTP towards the client is impaired; RTCP and client-to-server packets
// are relayed as they are. It does not support RTSP authentication, because it
// rewrites URLs and Transport headers.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8555", "address clients connect to")
	upstream := flag.String("upstream", "127.0.0.1:8554", "RTSP server to forward to")
	loss := flag.Float64("loss", 0, "probability (0-1) of dropping an RTP packet")
	delayMin := flag.Duration("delay-min", 0, "minimum time to hold each RTP packet")
	delayMax := flag.Duration("delay-max", 0, "maximum time to hold each RTP packet")
	spikeProb := flag.Float64("spike-prob", 0, "probability (0-1) that a packet is held an extra -spike (a stall in ordered mode)")
	spike := flag.Duration("spike", 0, "extra delay for a spiked packet")
	reorder := flag.Bool("reorder", false, "let delayed packets overtake earlier ones")
	dropIDR := flag.Int("drop-idr-fragment", 0, "drop one fragment of the Nth H.264 IDR frame (0: off)")
	seed := flag.Int64("seed", 1, "random seed, for repeatable runs")
	flag.Parse()

	if *loss < 0 || *loss > 1 {
		log.Fatal("-loss must be between 0 and 1")
	}
	im := newImpairment(*loss, *delayMin, *delayMax, *spikeProb, *spike, *reorder, *dropIDR, *seed)
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("listening on %s, forwarding to %s (loss %.3f, delay %v-%v, spike %v at %.3f, reorder %v, drop-idr-fragment %d)",
		*listen, *upstream, *loss, *delayMin, *delayMax, *spike, *spikeProb, *reorder, *dropIDR)

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt)
		<-sig
		im.mu.Lock()
		log.Printf("forwarded %d RTP packets, dropped %d (%d keyframe fragments)", im.Forwarded, im.Dropped, im.IDRDrops)
		im.mu.Unlock()
		os.Exit(0)
	}()

	for {
		c, err := ln.Accept()
		if err != nil {
			log.Fatal(err)
		}
		go (&proxyConn{client: c, upstreamAddr: *upstream, listenAddr: *listen, im: im}).run()
	}
}

// udpSession relays the UDP media of one SETUP.
type udpSession struct {
	clientRTP, clientRTCP *net.UDPAddr // where the client listens
	serverRTP, serverRTCP *net.UDPAddr // where the server sends from
	upRTP, upRTCP         *net.UDPConn // proxy sockets facing the server
	downRTP, downRTCP     *net.UDPConn // proxy sockets facing the client
	upPortA, downPortA    int
}

type proxyConn struct {
	client       net.Conn
	upstreamAddr string
	listenAddr   string
	im           *impairment

	mu       sync.Mutex
	methods  map[string]string      // CSeq -> method of the outstanding requests
	pending  map[string]*udpSession // CSeq -> session waiting for its SETUP response
	sessions []*udpSession
}

func (p *proxyConn) run() {
	defer func() { _ = p.client.Close() }()
	up, err := net.Dial("tcp", p.upstreamAddr)
	if err != nil {
		log.Printf("dial upstream: %v", err)
		return
	}
	defer func() { _ = up.Close() }()
	defer p.closeSessions()
	p.methods, p.pending = map[string]string{}, map[string]*udpSession{}

	done := make(chan struct{}, 2)
	go func() { p.pump(p.client, up, true); done <- struct{}{} }()
	go func() { p.pump(up, p.client, false); done <- struct{}{} }()
	<-done
}

func (p *proxyConn) closeSessions() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, s := range p.sessions {
		for _, c := range []*net.UDPConn{s.upRTP, s.upRTCP, s.downRTP, s.downRTCP} {
			_ = c.Close()
		}
	}
}

// pump copies messages from src to dst, rewriting what UDP needs.
func (p *proxyConn) pump(src, dst net.Conn, fromClient bool) {
	br := bufio.NewReader(src)
	for {
		m, err := readMessage(br)
		if err != nil {
			return
		}
		if m.frame == nil {
			if fromClient {
				err = p.request(m)
			} else {
				err = p.response(m)
			}
			if err != nil {
				log.Printf("rewrite: %v", err)
				return
			}
		}
		if _, err := dst.Write(m.bytes()); err != nil {
			return
		}
	}
}

func (p *proxyConn) swapAddr(m *message, from, to string) {
	m.start = strings.ReplaceAll(m.start, from, to)
	for i := range m.headers {
		m.headers[i] = strings.ReplaceAll(m.headers[i], from, to)
	}
}

func (p *proxyConn) request(m *message) error {
	p.swapAddr(m, p.listenAddr, p.upstreamAddr)
	cseq := m.header("CSeq")
	p.mu.Lock()
	p.methods[cseq] = m.method()
	p.mu.Unlock()
	if m.method() != "SETUP" {
		return nil
	}
	tr := m.header("Transport")
	if !udpTransport(tr) {
		return nil
	}
	ca, cb, _ := portPair(clientPortRe, tr)
	host, _, _ := net.SplitHostPort(p.client.RemoteAddr().String())
	s := &udpSession{
		clientRTP:  &net.UDPAddr{IP: net.ParseIP(host), Port: ca},
		clientRTCP: &net.UDPAddr{IP: net.ParseIP(host), Port: cb},
	}
	var err error
	if s.upRTP, s.upRTCP, err = listenPair(); err != nil {
		return err
	}
	if s.downRTP, s.downRTCP, err = listenPair(); err != nil {
		return err
	}
	s.upPortA = s.upRTP.LocalAddr().(*net.UDPAddr).Port
	s.downPortA = s.downRTP.LocalAddr().(*net.UDPAddr).Port
	m.setHeader("Transport", replacePorts(clientPortRe, "client_port", tr, s.upPortA, s.upPortA+1))
	p.mu.Lock()
	p.pending[cseq] = s
	p.sessions = append(p.sessions, s)
	p.mu.Unlock()
	return nil
}

func (p *proxyConn) response(m *message) error {
	p.swapAddr(m, p.upstreamAddr, p.listenAddr)
	cseq := m.header("CSeq")
	p.mu.Lock()
	s := p.pending[cseq]
	delete(p.pending, cseq)
	delete(p.methods, cseq)
	p.mu.Unlock()
	if s == nil {
		return nil
	}
	tr := m.header("Transport")
	sa, sb, ok := portPair(serverPortRe, tr)
	if !ok {
		return fmt.Errorf("SETUP response without server_port: %q", tr)
	}
	uh, _, _ := net.SplitHostPort(p.upstreamAddr)
	s.serverRTP = &net.UDPAddr{IP: net.ParseIP(uh), Port: sa}
	s.serverRTCP = &net.UDPAddr{IP: net.ParseIP(uh), Port: sb}
	m.setHeader("Transport", replacePorts(serverPortRe, "server_port", tr, s.downPortA, s.downPortA+1))
	s.start(p.im)
	return nil
}

// listenPair binds an even/odd pair of consecutive UDP ports, as RTP/RTCP need.
func listenPair() (*net.UDPConn, *net.UDPConn, error) {
	for range 200 {
		a, err := net.ListenUDP("udp", &net.UDPAddr{})
		if err != nil {
			return nil, nil, err
		}
		port := a.LocalAddr().(*net.UDPAddr).Port
		if port%2 == 0 {
			if b, err := net.ListenUDP("udp", &net.UDPAddr{Port: port + 1}); err == nil {
				return a, b, nil
			}
		}
		_ = a.Close()
	}
	return nil, nil, fmt.Errorf("no free UDP port pair")
}

func (s *udpSession) start(im *impairment) {
	// Server to client: RTP is impaired, RTCP is not.
	go relay(s.upRTP, s.downRTP, s.clientRTP, im)
	go relay(s.upRTCP, s.downRTCP, s.clientRTCP, nil)
	// Client to server: hole-punch packets and receiver reports.
	go relay(s.downRTP, s.upRTP, s.serverRTP, nil)
	go relay(s.downRTCP, s.upRTCP, s.serverRTCP, nil)
}

// relay reads from in and writes to dst through out, impairing when im is set.
func relay(in, out *net.UDPConn, dst *net.UDPAddr, im *impairment) {
	var fifo chan queued
	if im != nil && !im.reorder {
		fifo = make(chan queued, 4096)
		go func() {
			for q := range fifo {
				time.Sleep(time.Until(q.at))
				_, _ = out.WriteToUDP(q.data, dst)
			}
		}()
		defer close(fifo)
	}
	buf := make([]byte, 65535)
	for {
		n, _, err := in.ReadFromUDP(buf)
		if err != nil {
			return
		}
		pkt := append([]byte(nil), buf[:n]...)
		if im == nil {
			_, _ = out.WriteToUDP(pkt, dst)
			continue
		}
		drop, at := im.decide(pkt, time.Now())
		switch {
		case drop:
		case fifo != nil:
			select {
			case fifo <- queued{pkt, at}:
			default: // queue full: behave like a congested link
			}
		default:
			time.AfterFunc(time.Until(at), func() { _, _ = out.WriteToUDP(pkt, dst) })
		}
	}
}

type queued struct {
	data []byte
	at   time.Time
}

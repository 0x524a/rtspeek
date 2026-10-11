package main

import (
	"bufio"
	"strings"
	"testing"
	"time"
)

func TestReadMessageParsesRequestWithBodyAndFrame(t *testing.T) {
	in := "ANNOUNCE rtsp://h/p RTSP/1.0\r\nCSeq: 3\r\nContent-Length: 4\r\n\r\nabcd" + "$\x00\x00\x02hi"
	br := bufio.NewReader(strings.NewReader(in))
	m, err := readMessage(br)
	if err != nil || m.method() != "ANNOUNCE" || m.header("cseq") != "3" || string(m.body) != "abcd" {
		t.Fatalf("message = %+v, err %v", m, err)
	}
	if got := string(m.bytes()); got != "ANNOUNCE rtsp://h/p RTSP/1.0\r\nCSeq: 3\r\nContent-Length: 4\r\n\r\nabcd" {
		t.Fatalf("round trip = %q", got)
	}
	f, err := readMessage(br)
	if err != nil || string(f.frame) != "$\x00\x00\x02hi" {
		t.Fatalf("frame = %q, err %v", f.frame, err)
	}
}

func TestTransportRewrite(t *testing.T) {
	tr := "RTP/AVP;unicast;client_port=5000-5001"
	if !udpTransport(tr) || udpTransport("RTP/AVP/TCP;unicast;interleaved=0-1") {
		t.Fatal("udpTransport misclassified")
	}
	a, b, ok := portPair(clientPortRe, tr)
	if !ok || a != 5000 || b != 5001 {
		t.Fatalf("ports = %d %d %v", a, b, ok)
	}
	if got := replacePorts(clientPortRe, "client_port", tr, 7000, 7001); got != "RTP/AVP;unicast;client_port=7000-7001" {
		t.Fatalf("rewritten = %q", got)
	}
	m := &message{start: "RTSP/1.0 200 OK", headers: []string{"Transport: x"}}
	m.setHeader("transport", "y")
	m.setHeader("Session", "s")
	if m.header("Transport") != "y" || m.header("Session") != "s" || len(m.headers) != 2 {
		t.Fatalf("headers = %v", m.headers)
	}
}

func TestImpairmentLossIsSeededAndBounded(t *testing.T) {
	pkt := make([]byte, 20)
	count := func(seed int64) uint64 {
		im := newImpairment(0.1, 0, 0, 0, 0, false, 0, seed)
		for range 10000 {
			im.decide(pkt, time.Now())
		}
		return im.Dropped
	}
	if a, b := count(7), count(7); a != b {
		t.Fatalf("same seed gave %d and %d drops", a, b)
	}
	if d := count(7); d < 800 || d > 1200 {
		t.Fatalf("10%% loss dropped %d of 10000", d)
	}
}

func TestImpairmentOrderedDelayNeverOvertakes(t *testing.T) {
	im := newImpairment(0, 0, 100*time.Millisecond, 0, 0, false, 0, 1)
	pkt := make([]byte, 20)
	var last time.Time
	now := time.Now()
	for range 1000 {
		_, at := im.decide(pkt, now)
		if at.Before(last) {
			t.Fatal("ordered mode released a packet before an earlier one")
		}
		last = at
	}
}

// rtpFU builds an RTP packet carrying one H.264 FU-A fragment.
func rtpFU(start, end bool, nalType byte) []byte {
	p := make([]byte, 14)
	p[0] = 0x80
	p[12] = 0x7c // FU indicator, type 28
	p[13] = nalType
	if start {
		p[13] |= 0x80
	}
	if end {
		p[13] |= 0x40
	}
	return p
}

func TestImpairmentDropsOneFragmentOfTheChosenIDR(t *testing.T) {
	im := newImpairment(0, 0, 0, 0, 0, false, 2, 1)
	now := time.Now()
	var dropped []int
	for frame := 1; frame <= 3; frame++ {
		for i, p := range [][]byte{rtpFU(true, false, 5), rtpFU(false, false, 5), rtpFU(false, false, 5), rtpFU(false, true, 5)} {
			if drop, _ := im.decide(p, now); drop {
				dropped = append(dropped, frame*10+i)
			}
		}
	}
	if len(dropped) != 1 || dropped[0] != 21 {
		t.Fatalf("dropped %v, want only fragment 1 of frame 2", dropped)
	}
	if im.IDRDrops != 1 {
		t.Fatalf("IDRDrops = %d", im.IDRDrops)
	}
}

func TestImpairmentIgnoresShortAndNonFUPackets(t *testing.T) {
	im := newImpairment(0, 0, 0, 0, 0, false, 1, 1)
	if drop, _ := im.decide([]byte{0x80}, time.Now()); drop {
		t.Fatal("short packet dropped")
	}
	plain := make([]byte, 20)
	plain[12] = 0x65 // single-NAL IDR, not fragmented
	if drop, _ := im.decide(plain, time.Now()); drop {
		t.Fatal("unfragmented packet dropped")
	}
}

package rtspeek

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

// TestDescribeStreamConnectionClosed simulates a server that accepts a TCP connection
// but closes it before responding to DESCRIBE, triggering connection closed error.
func TestDescribeStreamConnectionClosed(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().String()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		r := bufio.NewReader(conn)
		// helper to read single RTSP request (lines until blank)
		readReq := func() (string, bool) {
			var first string
			for {
				line, err := r.ReadString('\n')
				if err != nil {
					return first, false
				}
				line = strings.TrimRight(line, "\r\n")
				if first == "" {
					first = line
				}
				if line == "" {
					break
				}
			}
			return first, true
		}
		// Read OPTIONS and ignore content
		if _, ok := readReq(); !ok {
			conn.Close()
			return
		}
		// Respond minimally to OPTIONS so client proceeds
		conn.Write([]byte("RTSP/1.0 200 OK\r\nCSeq: 1\r\nPublic: DESCRIBE\r\n\r\n"))
		// Read DESCRIBE request then close without responding
		readReq()
		// Close to produce EOF for response read
		conn.Close()
	}()

	url := "rtsp://" + addr + "/closed"
	info, err := DescribeStream(context.Background(), url, 1500*time.Millisecond)

	// Should get an error for connection closure
	if err == nil {
		t.Fatalf("expected connection closed error, got success")
	}

	// But should still have info about reachability
	if info == nil {
		t.Fatalf("expected info to be available even with connection failure")
	}

	if info.IsReachable() == false { // preflight dial should have succeeded
		t.Fatalf("expected reachable=true prior to closure")
	}
	if info.IsDescribeSucceeded() {
		t.Fatalf("expected describe failure on forcibly closed connection")
	}
}

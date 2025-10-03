package rtspeek

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestIsConnectableSuccess(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	url := "rtsp://" + ln.Addr().String() + "/any"
	ok, err := IsConnectable(context.Background(), url, 600*time.Millisecond)
	if err != nil || !ok {
		t.Fatalf("expected ok true no error, got ok=%v err=%v", ok, err)
	}
}

func TestIsConnectableInvalidURL(t *testing.T) {
	ok, err := IsConnectable(context.Background(), "http://bad", 300*time.Millisecond)
	if err == nil || ok {
		t.Fatalf("expected invalid URL error, got ok=%v err=%v", ok, err)
	}
}

func TestIsConnectableDNSError(t *testing.T) {
	// .invalid is guaranteed not to resolve
	ok, err := IsConnectable(context.Background(), "rtsp://doesnotexist.invalid/stream", 500*time.Millisecond)
	if err == nil || ok {
		t.Fatalf("expected DNS error, got ok=%v err=%v", ok, err)
	}
}

func TestIsConnectableConnectionRefused(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	l.Close() // free the port
	ok, err := IsConnectable(context.Background(), "rtsp://"+addr+"/x", 500*time.Millisecond)
	if err == nil || ok {
		t.Fatalf("expected connection refused error, got ok=%v err=%v", ok, err)
	}
}

func TestIsConnectableTimeout(t *testing.T) {
	// Use unroutable IP (TEST-NET-1) with short timeout to trigger timeout
	ok, err := IsConnectable(context.Background(), "rtsp://203.0.113.1:6553/stream", 200*time.Millisecond)
	if err == nil || ok {
		t.Fatalf("expected timeout/connection error, got ok=%v err=%v", ok, err)
	}
}

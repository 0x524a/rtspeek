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
	ok, fr, err := IsConnectable(context.Background(), url, 600*time.Millisecond)
	if err != nil || !ok || fr != "" {
		t.Fatalf("expected ok true no failure, got ok=%v fr=%q err=%v", ok, fr, err)
	}
}

func TestIsConnectableInvalidURL(t *testing.T) {
	ok, fr, err := IsConnectable(context.Background(), "http://bad", 300*time.Millisecond)
	if err == nil || ok || fr != "invalid_url" {
		t.Fatalf("expected invalid_url err, got ok=%v fr=%q err=%v", ok, fr, err)
	}
}

func TestIsConnectableDNSError(t *testing.T) {
	// .invalid is guaranteed not to resolve
	ok, fr, err := IsConnectable(context.Background(), "rtsp://doesnotexist.invalid/stream", 500*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ok || fr != "dns_error" {
		t.Fatalf("expected dns_error, got ok=%v fr=%q", ok, fr)
	}
}

func TestIsConnectableConnectionRefused(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	l.Close() // free the port
	ok, fr, err := IsConnectable(context.Background(), "rtsp://"+addr+"/x", 500*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ok || fr != "connection_refused" {
		t.Fatalf("expected connection_refused, got ok=%v fr=%q", ok, fr)
	}
}

func TestIsConnectableTimeout(t *testing.T) {
	// Use unroutable IP (TEST-NET-1) with short timeout to trigger timeout classification
	ok, fr, err := IsConnectable(context.Background(), "rtsp://203.0.113.1:6553/stream", 200*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ok || (fr != "timeout" && fr != "connection_refused") {
		t.Fatalf("expected timeout/connection_refused, got ok=%v fr=%q", ok, fr)
	}
}

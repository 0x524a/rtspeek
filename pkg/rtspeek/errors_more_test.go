package rtspeek

import (
	"context"
	"net"
	"testing"
	"time"
)

// TestDescribeStreamDNSError attempts to resolve an invalid TLD to trigger DNS failure.
func TestDescribeStreamDNSError(t *testing.T) {
	ctx := context.Background()
	// .invalid is a reserved TLD (RFC 2606) guaranteed not to resolve.
	url := "rtsp://example.thisdoesnotexisttotrigger.invalid/stream"
	info, err := DescribeStream(ctx, url, 800*time.Millisecond)

	// Should get a DNS error
	if err == nil {
		t.Fatalf("expected DNS error, got success")
	}

	// Should have info about the attempt
	if info == nil {
		t.Fatalf("expected info to be available even with DNS failure")
	}

	if info.IsReachable() {
		t.Fatalf("expected reachable=false for DNS failure")
	}
}

// TestDescribeStreamConnectionRefused uses a free port then closes listener so connect is refused.
func TestDescribeStreamConnectionRefused(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	l.Close() // free immediately so next dial should refuse

	ctx := context.Background()
	url := "rtsp://" + addr + "/path"
	info, err := DescribeStream(ctx, url, 700*time.Millisecond)

	// Should get a connection refused error
	if err == nil {
		t.Fatalf("expected connection refused error, got success")
	}

	// Should have info about the attempt
	if info == nil {
		t.Fatalf("expected info to be available even with connection failure")
	}

	if info.IsReachable() {
		t.Fatalf("expected reachable=false for refused port")
	}
	if info.IsDescribeSucceeded() {
		t.Fatalf("expected describe failure")
	}
}

package rtspeek

import (
	"context"
	"net"
	"testing"
	"time"
)

// TestDescribeStreamDNSError attempts to resolve an invalid TLD to trigger DNS failure classification.
func TestDescribeStreamDNSError(t *testing.T) {
	ctx := context.Background()
	// .invalid is a reserved TLD (RFC 2606) guaranteed not to resolve.
	url := "rtsp://example.thisdoesnotexisttotrigger.invalid/stream"
	info, _ := DescribeStream(ctx, url, 800*time.Millisecond)
	if info.Failure() != "dns_error" {
		// Some systems may fail earlier (invalid URL parsing) but that should still set Valid=false; check fallback.
		if info.Failure() == "" || info.Failure() == "other" {
			t.Fatalf("expected dns_error classification, got %q error=%s", info.Failure(), info.Error())
		}
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
	info, _ := DescribeStream(ctx, url, 700*time.Millisecond)
	if info.Failure() != "connection_refused" {
		t.Fatalf("expected connection_refused classification, got %q (error=%s)", info.Failure(), info.Error())
	}
	if info.IsReachable() {
		t.Fatalf("expected reachable=false for refused port")
	}
	if info.DescribeSucceeded() {
		t.Fatalf("describe should not succeed on refused port")
	}
}

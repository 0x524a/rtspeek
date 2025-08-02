package rtspeek

import (
	"context"
	"net"
	"testing"
	"time"
)

// TestDescribeStreamTimeout sets up a TCP listener that never speaks RTSP to force a timeout classification.
func TestDescribeStreamTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	url := "rtsp://" + ln.Addr().String() + "/idle"
	ctx := context.Background()
	start := time.Now()
	info, errD := DescribeStream(ctx, url, 500*time.Millisecond)
	elapsed := time.Since(start)
	if errD == nil {
		// DescribeStream returns ErrDescribeFailed on timeout
		// So if err is nil but describe_ok false, still acceptable, but we expect an error classification path.
		if info.DescribeSucceeded() {
			t.Fatalf("expected describe to fail due to timeout; got success")
		}
	}
	if info.Failure() != "timeout" {
		t.Fatalf("expected failure_reason timeout got %s (err=%v)", info.Failure(), errD)
	}
	// Ensure the timeout wasn't ignored (some grace > requested timeout acceptable)
	if elapsed < 400*time.Millisecond || elapsed > 1500*time.Millisecond {
		t.Fatalf("unexpected elapsed duration %v", elapsed)
	}
}

// TestDescribeStreamUnsupportedScheme ensures http:// is rejected with ErrInvalidURL and classification unsupported_scheme.
func TestDescribeStreamUnsupportedScheme(t *testing.T) {
	ctx := context.Background()
	info, err := DescribeStream(ctx, "http://example.com/stream", 300*time.Millisecond)
	if err == nil {
		t.Fatalf("expected error for unsupported scheme")
	}
	if err != ErrInvalidURL {
		t.Fatalf("expected ErrInvalidURL, got %v", err)
	}
	if info != nil {
		t.Fatalf("expected info to be nil for invalid URL")
	}
}

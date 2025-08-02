package rtspeek

import (
    "testing"
    "context"
    "time"
)

// TestDescribeStreamInvalidURL ensures invalid / unsupported scheme returns (nil, ErrInvalidURL)
func TestDescribeStreamInvalidURL(t *testing.T) {
    ctx := context.Background()
    info, err := DescribeStream(ctx, "http://not-rtsp.local/stream", 200*time.Millisecond)
    if err != ErrInvalidURL {
        t.Fatalf("expected ErrInvalidURL, got %v", err)
    }
    if info != nil {
        t.Fatalf("expected info=nil for invalid URL, got %#v", info)
    }
}

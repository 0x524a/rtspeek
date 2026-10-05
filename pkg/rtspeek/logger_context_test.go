package rtspeek

import (
	"bytes"
	"context"
	"testing"
)

func TestMediaProcessingWritesBuffer(t *testing.T) {
	var out bytes.Buffer
	l := NewLogger(LogLevelInfo, &out, false)

	l.MediaProcessing("video", 0, "H264", "1920x1080")

	if out.Len() == 0 {
		t.Fatal("expected MediaProcessing to write to the logger output")
	}
	if len(l.buffer) != 1 {
		t.Fatalf("expected 1 buffer entry, got %d", len(l.buffer))
	}
}

func TestMediaProcessingBelowLevelIsSilent(t *testing.T) {
	var out bytes.Buffer
	l := NewLogger(LogLevelWarn, &out, false)

	l.MediaProcessing("video", 0, "H264", "1920x1080")

	if out.Len() != 0 || len(l.buffer) != 0 {
		t.Fatal("expected no output below Info level")
	}
}

func TestWithLoggerRoundTrip(t *testing.T) {
	l := NewLogger(LogLevelDebug, &bytes.Buffer{}, false)
	got, ok := WithLogger(context.Background(), l).Value(loggerKey).(*Logger)
	if !ok || got != l {
		t.Fatal("WithLogger did not store the logger under loggerKey")
	}
}

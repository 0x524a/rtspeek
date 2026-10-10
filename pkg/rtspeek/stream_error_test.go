package rtspeek

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestStreamErrorKeepsMessageAndUnwraps(t *testing.T) {
	inner := errors.New("boom")
	err := newStreamError(ReasonTimeout, fmt.Errorf("wrapped: %w", inner))

	if err.Error() != "wrapped: boom" {
		t.Fatalf("message changed: %q", err.Error())
	}
	if !errors.Is(err, inner) {
		t.Fatal("Unwrap chain broken")
	}
	var se *StreamError
	if !errors.As(err, &se) || se.Reason != ReasonTimeout {
		t.Fatalf("errors.As/Reason failed: %+v", se)
	}
}

func TestNewStreamErrorNil(t *testing.T) {
	if newStreamError(ReasonOther, nil) != nil {
		t.Fatal("nil must stay nil")
	}
}

func TestFailureReason(t *testing.T) {
	// Recorded reason wins over what the text would classify as.
	tagged := newStreamError(ReasonAuthRequired, errors.New("no authentication methods available"))
	if got := FailureReason(tagged); got != ReasonAuthRequired {
		t.Fatalf("tagged: got %q", got)
	}
	// Survives further wrapping.
	if got := FailureReason(fmt.Errorf("outer: %w", tagged)); got != ReasonAuthRequired {
		t.Fatalf("wrapped: got %q", got)
	}
	// Untagged errors fall back to text classification.
	if got := FailureReason(errors.New("dial tcp: connection refused")); got != ReasonConnectionRefused {
		t.Fatalf("fallback: got %q", got)
	}
	if got := FailureReason(nil); got != "" {
		t.Fatalf("nil: got %q", got)
	}
}

func TestDescribeStreamTagsReason(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want string
	}{
		{"connection refused", "rtsp://127.0.0.1:1/stream", ReasonConnectionRefused},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := DescribeStream(context.Background(), c.url, 2*time.Second)
			if err == nil {
				t.Fatal("expected error")
			}
			var se *StreamError
			if !errors.As(err, &se) || se.Reason != c.want {
				t.Fatalf("got %+v, want reason %s", se, c.want)
			}
			if got := FailureReason(err); got != c.want {
				t.Fatalf("FailureReason=%q want %q", got, c.want)
			}
		})
	}
}

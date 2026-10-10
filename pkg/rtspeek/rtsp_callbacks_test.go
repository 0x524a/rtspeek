package rtspeek

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

// gortsplib prints to stderr when these callbacks are nil, so NewRTSPSession
// always installs them. They must route to the logger and tolerate no logger.
func TestSessionCallbacksLogAtDebug(t *testing.T) {
	var out bytes.Buffer
	s := NewRTSPSession(time.Second, NewLogger(LogLevelDebug, &out, false))

	s.client.OnPacketsLost(3)
	s.client.OnDecodeError(errors.New("bad rtp"))
	s.client.OnTransportSwitch(errors.New("no udp"))

	for _, want := range []string{"RTP packets lost", "bad rtp", "transport switch", "no udp"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("logger output missing %q: %s", want, out.String())
		}
	}
}

func TestSessionCallbacksWithoutLogger(t *testing.T) {
	s := NewRTSPSession(time.Second, nil)

	// Must not panic and must not be nil (nil would make gortsplib print to stderr).
	if s.client.OnPacketsLost == nil || s.client.OnDecodeError == nil || s.client.OnTransportSwitch == nil {
		t.Fatal("callbacks must be installed even without a logger")
	}
	s.client.OnPacketsLost(1)
	s.client.OnDecodeError(errors.New("x"))
	s.client.OnTransportSwitch(errors.New("y"))
}

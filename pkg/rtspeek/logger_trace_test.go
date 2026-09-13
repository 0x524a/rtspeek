package rtspeek

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
)

// TestDescribeStreamDebugTrace exercises the real Logger (logger.go) debug path used by
// the --debug CLI flag: NewLogger, RTSPRequest/RTSPResponse/Stage, and GetLegacyTrace.
func TestDescribeStreamDebugTrace(t *testing.T) {
	videoMedia := &description.Media{Type: description.MediaTypeVideo, Formats: []format.Format{
		&format.H264{PayloadTyp: 96, SPS: []byte{0x67, 0x42, 0x00, 0x1f}, PPS: []byte{0x68, 0xce, 0x06, 0xe2}},
	}}
	session := &description.Session{Medias: []*description.Media{videoMedia}}

	var server *gortsplib.Server
	server, url := startDynamicServer(t, func(ctx *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
		stream := &gortsplib.ServerStream{Server: server, Desc: session}
		if err := stream.Initialize(); err != nil {
			return nil, nil, err
		}
		return &base.Response{StatusCode: base.StatusOK}, stream, nil
	})
	defer server.Close()

	ctx := WithDebug(context.Background())
	info, err := DescribeStream(ctx, url, 1500*time.Millisecond)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if !info.IsDescribeSucceeded() {
		t.Fatalf("expected describe ok")
	}

	trace := info.GetDebugData()
	if len(trace) == 0 {
		t.Fatal("expected non-empty debug trace when debug is enabled")
	}

	var sawOptionsReq, sawDescribeReq, sawResponse bool
	for _, line := range trace {
		switch {
		case line == "--> OPTIONS "+url:
			sawOptionsReq = true
		case line == "--> DESCRIBE "+url:
			sawDescribeReq = true
		case strings.HasPrefix(line, "←"): // ←
			sawResponse = true
		}
	}
	if !sawOptionsReq {
		t.Errorf("expected an OPTIONS request line in trace, got %v", trace)
	}
	if !sawDescribeReq {
		t.Errorf("expected a DESCRIBE request line in trace, got %v", trace)
	}
	if !sawResponse {
		t.Errorf("expected at least one response line in trace, got %v", trace)
	}
}

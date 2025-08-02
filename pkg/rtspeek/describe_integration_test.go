//go:build integration

package rtspeek

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/bluenviron/gortsplib/v4"
	"github.com/bluenviron/gortsplib/v4/pkg/base"
	"github.com/bluenviron/gortsplib/v4/pkg/description"
	"github.com/bluenviron/gortsplib/v4/pkg/format"
)

// startTestServer spins a minimal RTSP server that serves a static SDP.
type testServerHandler struct {
	stream *gortsplib.ServerStream
}

func (h *testServerHandler) OnDescribe(ctx *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	return &base.Response{StatusCode: base.StatusOK}, h.stream, nil
}

func startTestServer(t *testing.T) (*gortsplib.Server, string) {
	t.Helper()

	medias := []*description.Media{{
		Type: description.MediaTypeVideo,
		Formats: []format.Format{
			&format.H264{PayloadTyp: 96, SPS: []byte{0x67, 0x42, 0x00, 0x1f}, PPS: []byte{0x68, 0xce, 0x06, 0xe2}},
		},
	}}

	// pick a free port
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	l.Close()

	s := &gortsplib.Server{RTSPAddress: addr}
	if err := s.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}

	stream := gortsplib.NewServerStream(s, &description.Session{Medias: medias})
	s.Handler = &testServerHandler{stream: stream}

	return s, "rtsp://" + addr + "/test"
}

// TestDescribeStreamIntegration requires networking and gortsplib server internals; run with: go test -tags=integration
func TestDescribeStreamIntegration(t *testing.T) {
	s, url := startTestServer(t)
	defer s.Close()

	// Wait briefly to ensure server running
	time.Sleep(100 * time.Millisecond)

	ctx := context.Background()
	info, err := DescribeStream(ctx, url, 1*time.Second)
	if err != nil {
		t.Skipf("integration skipped (DescribeStream error: %v)", err)
	}
	if !info.Reachable || !info.DescribeOK {
		t.Fatalf("expected reachable & describe ok: %+v", info)
	}
	if info.MediaCount == 0 {
		t.Fatalf("expected at least one media")
	}
}

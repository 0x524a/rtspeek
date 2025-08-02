package rtspeek

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/bluenviron/gortsplib/v4"
	"github.com/bluenviron/gortsplib/v4/pkg/base"
	"github.com/bluenviron/gortsplib/v4/pkg/description"
	"github.com/bluenviron/gortsplib/v4/pkg/format"
)

// dynamicHandler allows customizing server responses for each test case.
type dynamicHandler struct {
	onDescribe func(ctx *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error)
}

func (h *dynamicHandler) OnDescribe(ctx *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	return h.onDescribe(ctx)
}

// helper to start server with specific describe behavior
func startDynamicServer(t *testing.T, onDescribe func(*gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error)) (srv *gortsplib.Server, url string) {
	t.Helper()

	// pick free port
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	l.Close()

	srv = &gortsplib.Server{RTSPAddress: addr}
	if err := srv.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	srv.Handler = &dynamicHandler{onDescribe: onDescribe}
	return srv, "rtsp://" + addr + "/test"
}

func TestDescribeStreamTable(t *testing.T) {
	videoMedia := &description.Media{Type: description.MediaTypeVideo, Formats: []format.Format{&format.H264{PayloadTyp: 96, SPS: []byte{0x67, 0x42, 0x00, 0x1f}, PPS: []byte{0x68, 0xce, 0x06, 0xe2}}}}
	session := &description.Session{Medias: []*description.Media{videoMedia}}

	cases := []struct {
		name          string
		setup         func(*testing.T) (cleanup func(), url string)
		expectOK      bool
		expectFailure string
	}{
		{
			name: "success",
			setup: func(t *testing.T) (func(), string) {
				var server *gortsplib.Server
				server, url := startDynamicServer(t, func(ctx *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
					stream := gortsplib.NewServerStream(server, session)
					return &base.Response{StatusCode: base.StatusOK}, stream, nil
				})
				return func() { server.Close() }, url
			},
			expectOK: true,
		},
		{
			name: "not_found",
			setup: func(t *testing.T) (func(), string) {
				srv, url := startDynamicServer(t, func(ctx *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
					return &base.Response{StatusCode: base.StatusNotFound}, nil, nil
				})
				return func() { srv.Close() }, url
			},
			expectOK:      false,
			expectFailure: "not_found",
		},
		{
			name: "auth_required",
			setup: func(t *testing.T) (func(), string) {
				first := true
				var server *gortsplib.Server
				server, url := startDynamicServer(t, func(ctx *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
					// Simulate only DESCRIBE needing auth; OPTIONS should pass.
					if first {
						first = false
						return &base.Response{StatusCode: base.StatusUnauthorized, Header: base.Header{"Www-Authenticate": base.HeaderValue{"Digest realm=\"test\", nonce=\"abc\""}}}, nil, nil
					}
					stream := gortsplib.NewServerStream(server, session)
					return &base.Response{StatusCode: base.StatusOK}, stream, nil
				})
				return func() { server.Close() }, url
			},
			expectOK: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cleanup, url := tc.setup(t)
			defer cleanup()
			ctx := context.Background()
			// Add credentials for auth test
			if tc.name == "auth_required" {
				url = strings.Replace(url, "rtsp://", "rtsp://user:pass@", 1)
			}
			info, _ := DescribeStream(ctx, url, 1500*time.Millisecond)
			if tc.expectOK {
				if !info.DescribeSucceeded() {
					t.Fatalf("expected describe ok, got failure_reason=%s error=%s", info.Failure(), info.Error())
				}
			} else {
				if info.DescribeSucceeded() {
					t.Fatalf("expected failure, got success")
				}
				if tc.expectFailure != "" && info.Failure() != tc.expectFailure {
					t.Fatalf("expected failure_reason %s got %s", tc.expectFailure, info.Failure())
				}
			}
		})
	}
}

// H265 SPS test vectors
func TestParseH265SPS(t *testing.T) {
	// Minimal (fabricated) VPS/SPS NAL unit example (not fully realistic but enough for parser path)
	// Using a small synthetic SPS may cause parser to return an error; ensure we handle gracefully.
	sps := []byte{0x42, 0x01, 0x01, 0x60, 0x00, 0x00, 0x03, 0x00, 0x90, 0x00, 0x00, 0x03, 0x00, 0x00, 0x03, 0x00}
	_, _, _ = parseH265SPS(sps) // we only assert it does not panic
}

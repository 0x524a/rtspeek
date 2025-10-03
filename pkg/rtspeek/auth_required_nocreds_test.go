package rtspeek

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/bluenviron/gortsplib/v4"
	"github.com/bluenviron/gortsplib/v4/pkg/base"
)

// authAlwaysHandler always returns 401 Unauthorized to DESCRIBE requests.
type authAlwaysHandler struct{}

func (h *authAlwaysHandler) OnDescribe(ctx *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	return &base.Response{StatusCode: base.StatusUnauthorized, Header: base.Header{"Www-Authenticate": base.HeaderValue{"Digest realm=\"x\", nonce=\"y\""}}}, nil, nil
}

// TestDescribeStreamAuthRequiredNoCreds ensures authentication error when no credentials supplied.
func TestDescribeStreamAuthRequiredNoCreds(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	l.Close()

	srv := &gortsplib.Server{RTSPAddress: addr}
	if err := srv.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	defer srv.Close()
	srv.Handler = &authAlwaysHandler{}

	url := "rtsp://" + addr + "/needauth"
	info, err := DescribeStream(context.Background(), url, 1200*time.Millisecond)

	// Should get error for authentication failure
	if err == nil {
		t.Fatalf("expected authentication error, got success")
	}

	// But we should still have info about reachability
	if info == nil {
		t.Fatalf("expected info to be available even with auth failure")
	}

	if info.IsDescribeSucceeded() {
		t.Fatalf("describe should not succeed without credentials")
	}
	if !info.IsReachable() {
		t.Fatalf("expected reachable true (preflight dial ok)")
	}
}

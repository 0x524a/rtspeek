package rtspeek

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/bluenviron/gortsplib/v4/pkg/base"
)

// NetworkDialer handles TCP connectivity checks for RTSP endpoints.
type NetworkDialer struct {
	timeout time.Duration
}

// NewNetworkDialer creates a new network dialer with the specified timeout.
func NewNetworkDialer(timeout time.Duration) *NetworkDialer {
	return &NetworkDialer{timeout: timeout}
}

// CheckConnectivity performs a TCP dial to verify basic reachability.
// It validates the URL, ensures scheme is rtsp/rtsps, resolves host, applies default port 554 if absent,
// then attempts a dial within timeout.
func (nd *NetworkDialer) CheckConnectivity(ctx context.Context, rawURL string) (bool, error) {
	if !ValidateURL(rawURL) {
		return false, ErrInvalidURL
	}

	parsed, parseErr := base.ParseURL(rawURL)
	if parseErr != nil {
		return false, fmt.Errorf("invalid URL format: %w", parseErr)
	}

	if parsed.Scheme != "rtsp" && parsed.Scheme != "rtsps" {
		return false, fmt.Errorf("unsupported scheme '%s': only rtsp and rtsps are supported", parsed.Scheme)
	}

	hostPort := nd.normalizeHostPort(parsed.Host)

	dialer := &net.Dialer{Timeout: nd.timeout}
	dialCtx, cancel := context.WithTimeout(ctx, nd.timeout)
	defer cancel()

	conn, dialErr := dialer.DialContext(dialCtx, "tcp", hostPort)
	if dialErr != nil {
		return false, fmt.Errorf("connection failed to %s: %w", hostPort, dialErr)
	}

	_ = conn.Close()
	return true, nil
}

// normalizeHostPort adds default port 554 if not specified.
// It properly handles IPv6 addresses in brackets.
func (nd *NetworkDialer) normalizeHostPort(host string) string {
	// Check if this is an IPv6 address without port
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		return host + ":554"
	}

	// For IPv4 or hostname, check if port is already specified
	if strings.LastIndex(host, ":") == -1 {
		return host + ":554"
	}

	return host
}

// PreflightDial performs a quick TCP connectivity check before RTSP operations.
func (nd *NetworkDialer) PreflightDial(ctx context.Context, parsedURL *base.URL) error {
	hostPort := nd.normalizeHostPort(parsedURL.Host)

	dialer := &net.Dialer{Timeout: nd.timeout}
	dialCtx, cancel := context.WithTimeout(ctx, nd.timeout)
	defer cancel()

	conn, err := dialer.DialContext(dialCtx, "tcp", hostPort)
	if err != nil {
		return fmt.Errorf("preflight dial failed: %w", err)
	}

	_ = conn.Close()
	return nil
}

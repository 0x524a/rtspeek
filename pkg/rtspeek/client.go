package rtspeek

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/bluenviron/gortsplib/v4/pkg/base"
	"github.com/bluenviron/gortsplib/v4/pkg/description"
)

// DescribeStream performs connection and DESCRIBE, returning StreamInfo and underlying description pointer.
func DescribeStream(ctx context.Context, url string, timeout time.Duration) (StreamInfo, error) {
	info := &streamInfo{URL: url, Protocol: "rtsp"}
	start := time.Now()

	if !ValidateURL(url) {
		return nil, ErrInvalidURL
	}

	parsedURL, err := base.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("invalid URL format: %w", err)
	}

	// Enforce supported schemes early
	if parsedURL.Scheme != "rtsp" && parsedURL.Scheme != "rtsps" {
		return nil, fmt.Errorf("unsupported scheme '%s': only rtsp and rtsps are supported", parsedURL.Scheme)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	debugEnabled := isDebug(ctx)

	// Create logger if debug is enabled
	var logger *Logger
	if debugEnabled {
		logger = NewLogger(LogLevelDebug, io.Discard, false) // Discard for now, collected in buffer
	}

	// Perform preflight TCP connectivity check
	dialer := NewNetworkDialer(timeout)
	if preflightErr := dialer.PreflightDial(ctx, parsedURL); preflightErr != nil {
		info.Latency = float64(time.Since(start)) / float64(time.Millisecond)
		info.Reachable = false
		return info, fmt.Errorf("connection failed: %w", preflightErr)
	}
	info.Reachable = true

	// Perform RTSP operations with timeout handling
	resultCh := make(chan *rtspResult, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				resultCh <- &rtspResult{err: fmt.Errorf("RTSP operation panicked: %v", r)}
			}
		}()

		session := NewRTSPSession(timeout, logger)
		defer session.Close()

		desc, trace, sessionErr := session.PerformDescribe(ctx, parsedURL)
		resultCh <- &rtspResult{
			description: desc,
			trace:       trace,
			err:         sessionErr,
		}
	}()

	var result *rtspResult
	select {
	case <-ctx.Done():
		info.Latency = float64(time.Since(start)) / float64(time.Millisecond)
		if debugEnabled {
			// We may not have trace data if timeout occurred early
			info.DebugTrace = []string{"TIMEOUT: operation cancelled before completion"}
		}
		return info, fmt.Errorf("operation timed out after %v", timeout)
	case result = <-resultCh:
		// Continue with result processing
	}

	info.Latency = float64(time.Since(start)) / float64(time.Millisecond)

	if result.err != nil {
		if debugEnabled && result.trace != nil {
			info.DebugTrace = result.trace
		}
		return info, result.err
	}

	// Success: populate stream info with description
	info.DescribeOK = true
	info.RawDescription = result.description
	if debugEnabled && result.trace != nil {
		info.DebugTrace = result.trace
	}

	// Classify media streams
	processor := NewMediaProcessor()
	if logger != nil {
		if err := processor.ProcessMediasWithLogging(result.description, info, logger); err != nil {
			return nil, fmt.Errorf("media processing failed: %w", err)
		}
	} else {
		if err := processor.ProcessMedias(result.description, info); err != nil {
			return nil, fmt.Errorf("media processing failed: %w", err)
		}
	}

	return info, nil
}

// rtspResult encapsulates the result of RTSP operations.
type rtspResult struct {
	description *description.Session
	trace       []string
	err         error
}

// CheckReachable performs a quick DESCRIBE with a shorter timeout.
func CheckReachable(ctx context.Context, url string, timeout time.Duration) (bool, error) {
	return IsConnectable(ctx, url, timeout)
}

// IsConnectable performs only a TCP dial (no RTSP handshake) to determine basic reachability.
// It validates the URL, ensures scheme is rtsp/rtsps, resolves host, applies default port 554 if absent,
// then attempts a dial within timeout.
func IsConnectable(ctx context.Context, rawURL string, timeout time.Duration) (bool, error) {
	dialer := NewNetworkDialer(timeout)
	return dialer.CheckConnectivity(ctx, rawURL)
}

// debug context key and helpers
type debugCtxKey struct{}

var debugKey = debugCtxKey{}

func WithDebug(ctx context.Context) context.Context { return context.WithValue(ctx, debugKey, true) }
func isDebug(ctx context.Context) bool {
	v := ctx.Value(debugKey)
	b, _ := v.(bool)
	return b
}

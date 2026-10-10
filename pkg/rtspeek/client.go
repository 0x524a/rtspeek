package rtspeek

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
)

// DescribeStream performs connection and DESCRIBE, returning StreamInfo and underlying description pointer.
func DescribeStream(ctx context.Context, url string, timeout time.Duration) (StreamInfo, error) {
	return DescribeStreamWithOptions(ctx, url, Options{Timeout: timeout})
}

// DescribeStreamWithOptions is DescribeStream with extra options. When
// opts.Analyze is set it also receives the stream after DESCRIBE and attaches
// the result to the returned StreamInfo (see StreamInfo.GetAnalysis). Analysis
// pulls live media, which occupies a camera session. A failed analysis is
// reported in the Analysis, not as an error.
func DescribeStreamWithOptions(ctx context.Context, url string, opts Options) (StreamInfo, error) {
	timeout := opts.Timeout
	var analyzeOpts AnalyzeOptions
	if opts.Analyze != nil {
		var optErr error
		if analyzeOpts, optErr = opts.Analyze.normalize(); optErr != nil {
			return nil, optErr
		}
	}
	parent := ctx
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
		return nil, newStreamError(ReasonUnsupportedScheme,
			fmt.Errorf("unsupported scheme '%s': only rtsp and rtsps are supported", parsedURL.Scheme))
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	debugEnabled := isDebug(ctx)

	// Prefer a caller-supplied logger; otherwise create one if debug is enabled
	logger, ok := ctx.Value(loggerKey).(*Logger)
	if !ok || logger == nil {
		logger = nil
		if debugEnabled {
			logger = NewLogger(LogLevelDebug, io.Discard, false) // Discard for now, collected in buffer
		}
	}

	// Perform preflight TCP connectivity check
	dialer := NewNetworkDialer(timeout)
	if preflightErr := dialer.PreflightDial(ctx, parsedURL); preflightErr != nil {
		info.Latency = float64(time.Since(start)) / float64(time.Millisecond)
		info.Reachable = false
		return info, newStreamError(classifyError(preflightErr), fmt.Errorf("connection failed: %w", preflightErr))
	}
	info.Reachable = true

	// Perform RTSP operations with timeout handling
	session := NewRTSPSession(timeout, logger)
	// The goroutine below owns the client and closes it. Closing from here
	// could race with a describe that is still starting up after a timeout.
	// With analysis the client stays open until this function returns.
	sessionDone := make(chan struct{})
	defer close(sessionDone)
	resultCh := make(chan *rtspResult, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				resultCh <- &rtspResult{err: fmt.Errorf("RTSP operation panicked: %v", r)}
				session.Close()
			}
		}()

		desc, trace, sessionErr := session.PerformDescribe(ctx, parsedURL)
		resultCh <- &rtspResult{
			description: desc,
			trace:       trace,
			err:         sessionErr,
		}
		if opts.Analyze != nil && sessionErr == nil {
			<-sessionDone
		}
		session.Close()
	}()

	var result *rtspResult
	select {
	case <-ctx.Done():
		info.Latency = float64(time.Since(start)) / float64(time.Millisecond)
		if debugEnabled {
			// We may not have trace data if timeout occurred early
			info.DebugTrace = []string{"TIMEOUT: operation cancelled before completion"}
		}
		return info, newStreamError(ReasonTimeout, fmt.Errorf("operation timed out after %v", timeout))
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

	if opts.Analyze != nil {
		actx, acancel := context.WithTimeout(parent, analysisBudget(timeout, analyzeOpts))
		defer acancel()
		info.Analysis = session.Analyze(actx, result.description, analyzeOpts)
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

// debugCtxKey is the context key under which WithDebug stores its flag.
type debugCtxKey struct{}

var debugKey = debugCtxKey{}

// WithDebug returns a copy of ctx that makes stream inspection record a debug
// trace, available through StreamInfo.GetDebugData.
func WithDebug(ctx context.Context) context.Context { return context.WithValue(ctx, debugKey, true) }
func isDebug(ctx context.Context) bool {
	v := ctx.Value(debugKey)
	b, _ := v.(bool)
	return b
}

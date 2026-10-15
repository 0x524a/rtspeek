package rtspeek

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
)

// RTSPSession handles RTSP protocol operations (OPTIONS, DESCRIBE).
type RTSPSession struct {
	client  *gortsplib.Client
	logger  *Logger
	timeout time.Duration

	// switchedTransport is set when gortsplib falls back from UDP to TCP.
	switchedTransport atomic.Bool

	// server and methods come from the OPTIONS and DESCRIBE responses. They are
	// written by PerformDescribe and read after it returns.
	server  string
	methods []string
}

// NewRTSPSession creates a new RTSP session with the specified timeout and logger.
func NewRTSPSession(timeout time.Duration, logger *Logger) *RTSPSession {
	client := &gortsplib.Client{
		ReadTimeout:  timeout,
		WriteTimeout: timeout,
	}

	// Set up logging callbacks if logger is provided and debug level is enabled
	if logger != nil && logger.level >= LogLevelDebug {
		client.OnRequest = func(req *base.Request) {
			headers := make(map[string][]string)
			for k, v := range req.Header {
				headers[k] = []string(v)
			}
			logger.RTSPRequest(string(req.Method), RedactURL(req.URL.String()), headers)
		}

		client.OnResponse = func(res *base.Response) {
			headers := make(map[string][]string)
			for k, v := range res.Header {
				headers[k] = []string(v)
			}
			logger.RTSPResponse(int(res.StatusCode), res.StatusMessage, headers)
		}
	}

	// gortsplib prints to stderr when these are nil, so always install them.
	client.OnPacketsLost = func(lost uint64) {
		if logger != nil {
			logger.Debug("RTP packets lost", map[string]interface{}{"lost": lost})
		}
	}
	client.OnDecodeError = func(err error) {
		if logger != nil {
			logger.Debug("RTP decode error", map[string]interface{}{"error": err.Error()})
		}
	}
	rs := &RTSPSession{client: client, logger: logger, timeout: timeout}
	client.OnTransportSwitch = func(err error) {
		rs.switchedTransport.Store(true)
		if logger != nil {
			logger.Debug("transport switch", map[string]interface{}{"reason": err.Error()})
		}
	}

	return rs
}

// PerformDescribe executes the RTSP handshake (START, OPTIONS, DESCRIBE) with auth retry.
func (rs *RTSPSession) PerformDescribe(ctx context.Context, parsedURL *base.URL) (*description.Session, []string, error) {
	if rs.logger != nil {
		rs.logger.Stage("start")
	}

	rs.client.Scheme = parsedURL.Scheme
	rs.client.Host = parsedURL.Host

	start := time.Now()
	if err := rs.client.Start(); err != nil {
		if rs.logger != nil {
			rs.logger.NetworkOperation("rtsp_start", parsedURL.Host, time.Since(start), err)
		}
		return nil, rs.getTrace(), newStreamError(classifyError(err), fmt.Errorf("RTSP start failed: %w", err))
	}

	if rs.logger != nil {
		rs.logger.NetworkOperation("rtsp_start", parsedURL.Host, time.Since(start), nil)
	}

	if rs.logger != nil {
		rs.logger.Stage("options")
	}

	optionsStart := time.Now()
	optionsRes, err := rs.client.Options(parsedURL)
	rs.noteHeaders(optionsRes)
	if err != nil {
		if rs.logger != nil {
			rs.logger.NetworkOperation("rtsp_options", parsedURL.Host, time.Since(optionsStart), err)
		}
		if !isAuthChallenge(err) {
			return nil, rs.getTrace(), newStageError(StageOptions, classifyError(err), fmt.Errorf("RTSP options failed: %w", err))
		}
	} else if rs.logger != nil {
		rs.logger.NetworkOperation("rtsp_options", parsedURL.Host, time.Since(optionsStart), nil)
	}

	if rs.logger != nil {
		rs.logger.Stage("describe")
	}

	describeStart := time.Now()
	desc, describeRes, describeErr := rs.client.Describe(parsedURL)
	rs.noteHeaders(describeRes)
	if describeErr != nil && isAuthChallenge(describeErr) && parsedURL.User != nil {
		// Retry with authentication
		if rs.logger != nil {
			rs.logger.Stage("auth-retry")
		}

		retryStart := time.Now()
		desc2, retryRes, retryErr := rs.client.Describe(parsedURL)
		rs.noteHeaders(retryRes)
		if retryErr == nil {
			if rs.logger != nil {
				rs.logger.NetworkOperation("rtsp_describe_retry", parsedURL.Host, time.Since(retryStart), nil)
			}
			return desc2, rs.getTrace(), nil
		}
		if rs.logger != nil {
			rs.logger.NetworkOperation("rtsp_describe_retry", parsedURL.Host, time.Since(retryStart), retryErr)
		}
		describeErr = retryErr
	}

	if rs.logger != nil {
		rs.logger.NetworkOperation("rtsp_describe", parsedURL.Host, time.Since(describeStart), describeErr)
	}

	if describeErr != nil {
		reason := classifyError(describeErr)
		if isAuthChallenge(describeErr) {
			reason = ReasonAuthRequired
		}
		return nil, rs.getTrace(), newStageError(StageDescribe, reason, fmt.Errorf("RTSP describe failed: %w", describeErr))
	}

	return desc, rs.getTrace(), nil
}

// noteHeaders remembers the Server header and the OPTIONS Public list from a
// response. A nil response (connection error) changes nothing.
func (rs *RTSPSession) noteHeaders(res *base.Response) {
	if res == nil {
		return
	}
	if v := res.Header["Server"]; len(v) > 0 && rs.server == "" {
		rs.server = strings.TrimSpace(v[0])
	}
	if v := res.Header["Public"]; len(v) > 0 && len(rs.methods) == 0 {
		rs.methods = splitMethods(v)
	}
}

// splitMethods turns Public header values ("OPTIONS, DESCRIBE") into a list.
func splitMethods(values []string) []string {
	var out []string
	for _, v := range values {
		for _, m := range strings.Split(v, ",") {
			if m = strings.TrimSpace(m); m != "" {
				out = append(out, m)
			}
		}
	}
	return out
}

// getTrace returns the debug trace if logging is enabled (for backward compatibility).
func (rs *RTSPSession) getTrace() []string {
	if rs.logger != nil {
		return rs.logger.GetLegacyTrace()
	}
	return nil
}

// Close closes the RTSP client connection. PerformDescribe leaves the client
// open so the caller can keep using it, so the caller must call Close.
func (rs *RTSPSession) Close() {
	if rs.client != nil {
		rs.client.Close()
	}
}

package rtspeek

import (
	"context"
	"fmt"
	"time"

	"github.com/bluenviron/gortsplib/v4"
	"github.com/bluenviron/gortsplib/v4/pkg/base"
	"github.com/bluenviron/gortsplib/v4/pkg/description"
)

// RTSPSession handles RTSP protocol operations (OPTIONS, DESCRIBE).
type RTSPSession struct {
	client  *gortsplib.Client
	logger  *Logger
	timeout time.Duration
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
			logger.RTSPRequest(string(req.Method), req.URL.String(), headers)
		}

		client.OnResponse = func(res *base.Response) {
			headers := make(map[string][]string)
			for k, v := range res.Header {
				headers[k] = []string(v)
			}
			logger.RTSPResponse(int(res.StatusCode), res.StatusMessage, headers)
		}
	}

	return &RTSPSession{
		client:  client,
		logger:  logger,
		timeout: timeout,
	}
}

// PerformDescribe executes the RTSP handshake (START, OPTIONS, DESCRIBE) with auth retry.
func (rs *RTSPSession) PerformDescribe(ctx context.Context, parsedURL *base.URL) (*description.Session, []string, error) {
	if rs.logger != nil {
		rs.logger.Stage("start")
	}

	start := time.Now()
	if err := rs.client.Start(parsedURL.Scheme, parsedURL.Host); err != nil {
		if rs.logger != nil {
			rs.logger.NetworkOperation("rtsp_start", parsedURL.Host, time.Since(start), err)
		}
		return nil, rs.getTrace(), fmt.Errorf("RTSP start failed: %w", err)
	}

	if rs.logger != nil {
		rs.logger.NetworkOperation("rtsp_start", parsedURL.Host, time.Since(start), nil)
	}

	// Ensure client is closed when we're done
	defer func() {
		go rs.client.Close() // Non-blocking close
	}()

	if rs.logger != nil {
		rs.logger.Stage("options")
	}

	optionsStart := time.Now()
	if _, err := rs.client.Options(parsedURL); err != nil {
		if rs.logger != nil {
			rs.logger.NetworkOperation("rtsp_options", parsedURL.Host, time.Since(optionsStart), err)
		}
		if !isAuthChallenge(err) {
			return nil, rs.getTrace(), fmt.Errorf("RTSP options failed: %w", err)
		}
	} else if rs.logger != nil {
		rs.logger.NetworkOperation("rtsp_options", parsedURL.Host, time.Since(optionsStart), nil)
	}

	if rs.logger != nil {
		rs.logger.Stage("describe")
	}

	describeStart := time.Now()
	desc, _, describeErr := rs.client.Describe(parsedURL)
	if describeErr != nil && isAuthChallenge(describeErr) && parsedURL.User != nil {
		// Retry with authentication
		if rs.logger != nil {
			rs.logger.Stage("auth-retry")
		}

		retryStart := time.Now()
		desc2, _, retryErr := rs.client.Describe(parsedURL)
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
		return nil, rs.getTrace(), fmt.Errorf("RTSP describe failed: %w", describeErr)
	}

	return desc, rs.getTrace(), nil
}

// getTrace returns the debug trace if logging is enabled (for backward compatibility).
func (rs *RTSPSession) getTrace() []string {
	if rs.logger != nil {
		return rs.logger.GetLegacyTrace()
	}
	return nil
}

// Close closes the RTSP client connection.
func (rs *RTSPSession) Close() {
	if rs.client != nil {
		rs.client.Close()
	}
}

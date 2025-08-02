package rtspeek

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/bluenviron/gortsplib/v4"
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

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	debugEnabled := isDebug(ctx)
	var trace []string

	c := gortsplib.Client{ReadTimeout: timeout, WriteTimeout: timeout}
	if debugEnabled {
		c.OnRequest = func(req *base.Request) {
			trace = append(trace, fmt.Sprintf("--> %s %s", req.Method, req.URL))
			for k, v := range req.Header {
				trace = append(trace, fmt.Sprintf("--> H %s: %s", k, v))
			}
		}
		c.OnResponse = func(res *base.Response) {
			trace = append(trace, fmt.Sprintf("<-- %d %s", res.StatusCode, res.StatusMessage))
			for k, v := range res.Header {
				trace = append(trace, fmt.Sprintf("<-- H %s: %s", k, v))
			}
		}
	}

	parsedURL, err := base.ParseURL(url)
	if err != nil {
		return nil, ErrInvalidURL
	}
	// enforce supported schemes early
	if parsedURL.Scheme != "rtsp" && parsedURL.Scheme != "rtsps" {
		return nil, ErrInvalidURL
	}

	// Preflight TCP dial for reachability separate from RTSP sequence
	hostPort := parsedURL.Host
	if !strings.Contains(hostPort, ":") {
		hostPort += ":554"
	}
	if dconn, derr := (&net.Dialer{Timeout: timeout}).DialContext(ctx, "tcp", hostPort); derr != nil {
		info.Latency = float64(time.Since(start)) / float64(time.Millisecond)
		info.Reachable = false
		info.FailureReason = classifyError(derr)
		info.ErrorMessage = derr.Error()
		return info, ErrDescribeFailed
	} else {
		_ = dconn.Close()
		info.Reachable = true
	}

	// run handshake + Describe in goroutine for timeout respect
	type describeResult struct {
		desc *description.Session
		err  error
	}
	resCh := make(chan describeResult, 1)
	go func() {
		defer func() { recover() }()
		if debugEnabled {
			trace = append(trace, "STAGE: start")
		}
		if err := c.Start(parsedURL.Scheme, parsedURL.Host); err != nil {
			resCh <- describeResult{err: err}
			return
		}
		if debugEnabled {
			trace = append(trace, "STAGE: options")
		}
		if _, err := c.Options(parsedURL); err != nil {
			if !isAuthChallenge(err) {
				resCh <- describeResult{err: err}
				return
			}
		}
		if debugEnabled {
			trace = append(trace, "STAGE: describe")
		}
		desc, _, derr := c.Describe(parsedURL)
		if derr != nil && isAuthChallenge(derr) && parsedURL.User != nil {
			if debugEnabled {
				trace = append(trace, "STAGE: auth-retry")
			}
			desc2, _, derr2 := c.Describe(parsedURL)
			if derr2 == nil {
				resCh <- describeResult{desc: desc2}
				return
			}
			derr = derr2
		}
		resCh <- describeResult{desc: desc, err: derr}
	}()

	var desc *description.Session
	select {
	case <-ctx.Done():
		info.Latency = float64(time.Since(start)) / float64(time.Millisecond)
		info.FailureReason = "timeout"
		info.ErrorMessage = context.DeadlineExceeded.Error()
		// attempt to close client (best effort)
		go func() { c.Close() }()
		if debugEnabled {
			info.DebugTrace = trace
		}
		return info, ErrDescribeFailed
	case r := <-resCh:
		if r.err != nil {
			info.Latency = float64(time.Since(start)) / float64(time.Millisecond)
			if errors.Is(r.err, context.DeadlineExceeded) {
				info.FailureReason = "timeout"
				info.ErrorMessage = r.err.Error()
				return info, ErrDescribeFailed
			}
			fr := classifyError(r.err)
			if fr == "other" && strings.Contains(strings.ToLower(r.err.Error()), "unsupported scheme") {
				fr = "unsupported_scheme"
			}
			info.FailureReason = fr
			info.ErrorMessage = r.err.Error()
			return info, ErrDescribeFailed
		}
		desc = r.desc
	}
	info.Reachable = true
	info.DescribeOK = true
	info.Latency = float64(time.Since(start)) / float64(time.Millisecond)
	info.RawDescription = desc
	if debugEnabled {
		info.DebugTrace = trace
	}

	// classify medias
	for i, m := range desc.Medias {
		mi := classifyMedia(i, m)
		info.MediaCount++
		switch m.Type {
		case description.MediaTypeVideo:
			info.VideoMedias = append(info.VideoMedias, mi)
		case description.MediaTypeAudio:
			info.AudioMedias = append(info.AudioMedias, mi)
		default:
			info.OtherMedias = append(info.OtherMedias, mi)
		}
	}

	return info, nil
}

// CheckReachable performs a quick DESCRIBE with a shorter timeout.
func CheckReachable(ctx context.Context, url string, timeout time.Duration) (bool, error) {
	ok, _, err := IsConnectable(ctx, url, timeout)
	return ok, err
}

// IsConnectable performs only a TCP dial (no RTSP handshake) to determine basic reachability.
// It validates the URL, ensures scheme is rtsp/rtsps, resolves host, applies default port 554 if absent,
// then attempts a dial within timeout. It returns:
//
//	ok = true when the TCP connection is established and immediately closed.
//	failureReason = short classification (dns_error, connection_refused, timeout, unsupported_scheme, invalid_url, other)
//	err = ErrInvalidURL for invalid URLs, nil otherwise (network failures are reflected via failureReason only)
func IsConnectable(ctx context.Context, rawURL string, timeout time.Duration) (ok bool, failureReason string, err error) {
	if !ValidateURL(rawURL) {
		return false, "invalid_url", ErrInvalidURL
	}
	parsed, perr := base.ParseURL(rawURL)
	if perr != nil {
		return false, "invalid_url", ErrInvalidURL
	}
	if parsed.Scheme != "rtsp" && parsed.Scheme != "rtsps" {
		return false, "unsupported_scheme", ErrInvalidURL
	}
	hostPort := parsed.Host
	if !strings.Contains(hostPort, ":") {
		hostPort += ":554"
	}
	d := &net.Dialer{Timeout: timeout}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, derr := d.DialContext(ctx, "tcp", hostPort)
	if derr != nil {
		return false, classifyError(derr), nil
	}
	_ = conn.Close()
	return true, "", nil
}

// classifyError converts common network / RTSP errors into a short reason string.
func classifyError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	lmsg := strings.ToLower(msg)
	switch {
	case strings.Contains(lmsg, "connection refused"):
		return "connection_refused"
	case strings.Contains(lmsg, "i/o timeout") || strings.Contains(lmsg, "deadline exceeded") || strings.Contains(lmsg, "request timed out"):
		return "timeout"
	case strings.Contains(lmsg, "no such host"):
		return "dns_error"
	case strings.Contains(lmsg, "401") || strings.Contains(lmsg, "unauthorized"):
		return "auth_required"
	case strings.Contains(lmsg, "not found") || strings.Contains(lmsg, "404"):
		return "not_found"
	case strings.Contains(lmsg, "closed") || strings.Contains(lmsg, "broken pipe") || strings.Contains(lmsg, "use of closed network connection") || errors.Is(err, io.EOF):
		return "connection_closed"
	default:
		return "other"
	}
}

// auth challenge detection heuristic
func isAuthChallenge(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "401") || strings.Contains(msg, "unauthorized")
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

package rtspeek

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

var (
	ErrInvalidURL = errors.New("invalid rtsp url")
)

// Failure reasons returned by ErrorClassifier.Classify and carried on StreamError.
const (
	ReasonConnectionRefused = "connection_refused"
	ReasonTimeout           = "timeout"
	ReasonDNSError          = "dns_error"
	ReasonConnectionClosed  = "connection_closed"
	ReasonAuthRequired      = "auth_required"
	ReasonNotFound          = "not_found"
	ReasonUnsupportedScheme = "unsupported_scheme"
	ReasonOther             = "other"
)

// StreamError is an error that carries its failure reason, recorded where the
// failure happened. Its message is the wrapped error's message unchanged.
type StreamError struct {
	Reason string // one of the Reason* constants
	Err    error
}

func (e *StreamError) Error() string { return e.Err.Error() }
func (e *StreamError) Unwrap() error { return e.Err }

// newStreamError tags err with reason. A nil err stays nil.
func newStreamError(reason string, err error) error {
	if err == nil {
		return nil
	}
	return &StreamError{Reason: reason, Err: err}
}

// FailureReason returns the failure reason for err: the reason recorded on a
// StreamError if there is one, otherwise the text-based Classify result.
func FailureReason(err error) string {
	var se *StreamError
	if errors.As(err, &se) {
		return se.Reason
	}
	return globalClassifier.Classify(err)
}

// ErrorClassifier provides structured error classification for RTSP operations.
type ErrorClassifier struct{}

// NewErrorClassifier creates a new error classifier.
func NewErrorClassifier() *ErrorClassifier {
	return &ErrorClassifier{}
}

// Classify converts common network/RTSP errors into structured failure reasons.
func (ec *ErrorClassifier) Classify(err error) string {
	if err == nil {
		return ""
	}

	msg := err.Error()
	lowerMsg := strings.ToLower(msg)

	// Network-level errors
	if strings.Contains(lowerMsg, "connection refused") {
		return ReasonConnectionRefused
	}

	if strings.Contains(lowerMsg, "i/o timeout") ||
		strings.Contains(lowerMsg, "deadline exceeded") ||
		strings.Contains(lowerMsg, "request timed out") {
		return ReasonTimeout
	}

	if strings.Contains(lowerMsg, "no such host") {
		return ReasonDNSError
	}

	// Connection issues
	if strings.Contains(lowerMsg, "closed") ||
		strings.Contains(lowerMsg, "broken pipe") ||
		strings.Contains(lowerMsg, "use of closed network connection") ||
		errors.Is(err, io.EOF) {
		return ReasonConnectionClosed
	}

	// RTSP/HTTP status errors
	if strings.Contains(lowerMsg, "401") || strings.Contains(lowerMsg, "unauthorized") {
		return ReasonAuthRequired
	}

	if strings.Contains(lowerMsg, "not found") || strings.Contains(lowerMsg, "404") {
		return ReasonNotFound
	}

	// Protocol errors
	if strings.Contains(lowerMsg, "unsupported scheme") {
		return ReasonUnsupportedScheme
	}

	return ReasonOther
}

// WrapWithContext adds context to an error for better debugging.
func (ec *ErrorClassifier) WrapWithContext(err error, operation string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s failed: %w", operation, err)
}

// IsAuthChallenge detects if an error indicates an authentication challenge.
func (ec *ErrorClassifier) IsAuthChallenge(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "401") || strings.Contains(msg, "unauthorized")
}

// Global classifier instance for backward compatibility
var globalClassifier = NewErrorClassifier()

// classifyError provides backward compatibility with the original function.
func classifyError(err error) string {
	return globalClassifier.Classify(err)
}

// isAuthChallenge provides backward compatibility with the original function.
func isAuthChallenge(err error) bool {
	return globalClassifier.IsAuthChallenge(err)
}

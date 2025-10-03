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
		return "connection_refused"
	}

	if strings.Contains(lowerMsg, "i/o timeout") ||
		strings.Contains(lowerMsg, "deadline exceeded") ||
		strings.Contains(lowerMsg, "request timed out") {
		return "timeout"
	}

	if strings.Contains(lowerMsg, "no such host") {
		return "dns_error"
	}

	// Connection issues
	if strings.Contains(lowerMsg, "closed") ||
		strings.Contains(lowerMsg, "broken pipe") ||
		strings.Contains(lowerMsg, "use of closed network connection") ||
		errors.Is(err, io.EOF) {
		return "connection_closed"
	}

	// RTSP/HTTP status errors
	if strings.Contains(lowerMsg, "401") || strings.Contains(lowerMsg, "unauthorized") {
		return "auth_required"
	}

	if strings.Contains(lowerMsg, "not found") || strings.Contains(lowerMsg, "404") {
		return "not_found"
	}

	// Protocol errors
	if strings.Contains(lowerMsg, "unsupported scheme") {
		return "unsupported_scheme"
	}

	return "other"
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

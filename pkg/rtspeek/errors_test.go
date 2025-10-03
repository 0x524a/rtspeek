package rtspeek

import (
	"errors"
	"io"
	"testing"
)

func TestErrorClassifier_Classify(t *testing.T) {
	classifier := NewErrorClassifier()

	testCases := []struct {
		name     string
		err      error
		expected string
	}{
		{"nil_error", nil, ""},
		{"connection_refused", errors.New("connect: connection refused"), "connection_refused"},
		{"timeout_io", errors.New("i/o timeout"), "timeout"},
		{"timeout_deadline", errors.New("context deadline exceeded"), "timeout"},
		{"dns_error", errors.New("no such host"), "dns_error"},
		{"auth_required", errors.New("401 Unauthorized"), "auth_required"},
		{"not_found", errors.New("404 not found"), "not_found"},
		{"connection_closed", errors.New("use of closed network connection"), "connection_closed"},
		{"eof_error", io.EOF, "connection_closed"},
		{"unsupported_scheme", errors.New("unsupported scheme"), "unsupported_scheme"},
		{"unknown_error", errors.New("some random error"), "other"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := classifier.Classify(tc.err)
			if result != tc.expected {
				t.Errorf("Classify(%v) = %q, expected %q", tc.err, result, tc.expected)
			}
		})
	}
}

func TestErrorClassifier_WrapWithContext(t *testing.T) {
	classifier := NewErrorClassifier()

	// Test nil error
	result := classifier.WrapWithContext(nil, "test operation")
	if result != nil {
		t.Error("WrapWithContext should return nil for nil input")
	}

	// Test error wrapping
	originalErr := errors.New("original error")
	wrappedErr := classifier.WrapWithContext(originalErr, "test operation")

	if wrappedErr == nil {
		t.Fatal("WrapWithContext should not return nil for non-nil input")
	}

	expectedMsg := "test operation failed: original error"
	if wrappedErr.Error() != expectedMsg {
		t.Errorf("Expected wrapped error message %q, got %q", expectedMsg, wrappedErr.Error())
	}

	// Test that original error is preserved
	if !errors.Is(wrappedErr, originalErr) {
		t.Error("Wrapped error should preserve the original error for errors.Is checks")
	}
}

func TestErrorClassifier_IsAuthChallenge(t *testing.T) {
	classifier := NewErrorClassifier()

	testCases := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil_error", nil, false},
		{"auth_401", errors.New("401 Unauthorized"), true},
		{"auth_unauthorized", errors.New("unauthorized access"), true},
		{"mixed_case", errors.New("401 UNAUTHORIZED"), true},
		{"not_auth", errors.New("500 Internal Server Error"), false},
		{"empty_error", errors.New(""), false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := classifier.IsAuthChallenge(tc.err)
			if result != tc.expected {
				t.Errorf("IsAuthChallenge(%v) = %t, expected %t", tc.err, result, tc.expected)
			}
		})
	}
}

func TestGlobalClassifierBackwardCompatibility(t *testing.T) {
	// Test that global functions work the same as instance methods
	testErr := errors.New("connection refused")

	instanceResult := NewErrorClassifier().Classify(testErr)
	globalResult := classifyError(testErr)

	if instanceResult != globalResult {
		t.Errorf("Global classifyError and instance Classify should return same result")
	}

	authErr := errors.New("401 Unauthorized")
	instanceAuth := NewErrorClassifier().IsAuthChallenge(authErr)
	globalAuth := isAuthChallenge(authErr)

	if instanceAuth != globalAuth {
		t.Errorf("Global isAuthChallenge and instance IsAuthChallenge should return same result")
	}
}

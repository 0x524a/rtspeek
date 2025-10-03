package rtspeek

import (
	"context"
	"testing"
	"time"

	"github.com/bluenviron/gortsplib/v4/pkg/base"
)

func TestNetworkDialer_CheckConnectivity(t *testing.T) {
	dialer := NewNetworkDialer(1 * time.Second)

	testCases := []struct {
		name          string
		url           string
		expectOK      bool
		expectedError error
	}{
		{
			name:          "invalid_url_empty",
			url:           "",
			expectOK:      false,
			expectedError: ErrInvalidURL,
		},
		{
			name:          "invalid_url_wrong_scheme",
			url:           "http://example.com",
			expectOK:      false,
			expectedError: ErrInvalidURL,
		},
		{
			name:     "valid_unreachable",
			url:      "rtsp://nonexistent-host-12345.invalid:554/stream",
			expectOK: false,
			// Should get a connection error, not necessarily a specific type
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			ok, err := dialer.CheckConnectivity(ctx, tc.url)

			if ok != tc.expectOK {
				t.Errorf("expected ok=%v, got %v", tc.expectOK, ok)
			}

			if tc.expectedError != nil {
				if err != tc.expectedError {
					t.Errorf("expected error=%v, got %v", tc.expectedError, err)
				}
			} else {
				// For cases where we expect some error but don't specify which
				if tc.expectOK == false && err == nil {
					t.Error("expected an error but got none")
				}
			}
		})
	}
}

func TestNetworkDialer_normalizeHostPort(t *testing.T) {
	dialer := NewNetworkDialer(1 * time.Second)

	testCases := []struct {
		input    string
		expected string
	}{
		{"example.com", "example.com:554"},
		{"example.com:8554", "example.com:8554"},
		{"192.168.1.100", "192.168.1.100:554"},
		{"192.168.1.100:8080", "192.168.1.100:8080"},
		{"[::1]", "[::1]:554"},
		{"[::1]:8080", "[::1]:8080"},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := dialer.normalizeHostPort(tc.input)
			if result != tc.expected {
				t.Errorf("normalizeHostPort(%q) = %q, expected %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestNetworkDialer_PreflightDial(t *testing.T) {
	dialer := NewNetworkDialer(500 * time.Millisecond)

	// Test with invalid host that should fail quickly
	parsedURL, err := base.ParseURL("rtsp://nonexistent-host-12345.invalid:554/stream")
	if err != nil {
		t.Fatalf("Failed to parse URL: %v", err)
	}

	ctx := context.Background()
	err = dialer.PreflightDial(ctx, parsedURL)
	if err == nil {
		t.Error("Expected preflight dial to fail for nonexistent host")
	}

	if err != nil {
		errStr := err.Error()
		if errStr != "" {
			// Good, we got an error message
		}
	}
}

package rtspeek

import (
	"net/url"
	"strings"
)

// ValidateURL returns true if the provided string is a syntactically valid RTSP(S) URL.
func ValidateURL(raw string) bool {
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme != "rtsp" && u.Scheme != "rtsps" {
		return false
	}
	if u.Host == "" {
		return false
	}
	// minimal path requirement (some cameras accept empty path; allow but normalize)
	if !strings.HasPrefix(u.Path, "/") && u.Path != "" {
		return false
	}
	return true
}

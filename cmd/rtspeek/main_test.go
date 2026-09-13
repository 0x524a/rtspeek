package main

import (
	"testing"

	rtpeek "github.com/0x524A/rtspeek/pkg/rtspeek"
)

func TestParseLogLevel(t *testing.T) {
	cases := []struct {
		in   string
		want rtpeek.LogLevel
	}{
		{"trace", rtpeek.LogLevelTrace},
		{"debug", rtpeek.LogLevelDebug},
		{"info", rtpeek.LogLevelInfo},
		{"warn", rtpeek.LogLevelWarn},
		{"warning", rtpeek.LogLevelWarn},
		{"error", rtpeek.LogLevelError},
		{"disabled", rtpeek.LogLevelDisabled},
		{"nonsense", rtpeek.LogLevelDisabled},
		{"DEBUG", rtpeek.LogLevelDebug},
	}
	for _, c := range cases {
		if got := parseLogLevel(c.in); got != c.want {
			t.Errorf("parseLogLevel(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestNewAppVersion(t *testing.T) {
	app := newApp()
	if app.Version != version {
		t.Fatalf("app.Version = %q, want %q", app.Version, version)
	}
	if app.Name != "rtpeek" {
		t.Fatalf("app.Name = %q, want %q", app.Name, "rtpeek")
	}
}

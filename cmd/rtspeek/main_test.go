package main

import (
	"io"
	"os"
	"strings"
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

func TestRun_MissingRequiredURL(t *testing.T) {
	if code := run([]string{"rtspeek"}); code != 1 {
		t.Fatalf("run with no --url: got exit code %d, want 1", code)
	}
}

func TestRun_Help(t *testing.T) {
	if code := run([]string{"rtspeek", "--help"}); code != 0 {
		t.Fatalf("run with --help: got exit code %d, want 0", code)
	}
}

// captureStdout runs fn while os.Stdout is redirected, returning what was written.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()
	_ = w.Close()
	out, _ := io.ReadAll(r)
	return string(out)
}

func TestRunMissingURLFails(t *testing.T) {
	if code := run([]string{"rtspeek"}); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

func TestRunUnreachableReportsError(t *testing.T) {
	var code int
	out := captureStdout(t, func() {
		code = run([]string{"rtspeek", "--url", "rtsp://127.0.0.1:1/stream", "--timeout", "1s", "--pretty=false", "--verbose"})
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (error is reported in JSON)", code)
	}
	if !strings.Contains(out, `"describe_ok":false`) || !strings.Contains(out, `"error"`) {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestRunInvalidURLWritesErrorOutput(t *testing.T) {
	out := captureStdout(t, func() {
		run([]string{"rtspeek", "--url", "not a url", "--pretty=false"})
	})
	if !strings.Contains(out, `"error"`) {
		t.Fatalf("expected error JSON, got %q", out)
	}
}

func TestRunWithLoggingFlags(t *testing.T) {
	out := captureStdout(t, func() {
		run([]string{"rtspeek", "--url", "rtsp://127.0.0.1:1/stream", "--timeout", "1s", "--pretty=false",
			"--log-level", "debug", "--log-console"})
	})
	if !strings.Contains(out, `"error"`) {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestRunAnalyzeRejectsUnknownTransport(t *testing.T) {
	out := captureStdout(t, func() {
		run([]string{"rtspeek", "--url", "rtsp://127.0.0.1:1/stream", "--analyze", "--analyze-transport", "carrier-pigeon", "--pretty=false"})
	})
	if !strings.Contains(out, "unknown analyze transport") {
		t.Fatalf("expected a transport error in the output, got %q", out)
	}
}

func TestRunAnalyzeRejectsTooLongDuration(t *testing.T) {
	out := captureStdout(t, func() {
		run([]string{"rtspeek", "--url", "rtsp://127.0.0.1:1/stream", "--analyze", "--analyze-duration", "5m", "--pretty=false"})
	})
	if !strings.Contains(out, "exceeds the maximum") {
		t.Fatalf("expected a duration error in the output, got %q", out)
	}
}

func TestRunAnalyzeUnreachableStillReportsError(t *testing.T) {
	out := captureStdout(t, func() {
		run([]string{"rtspeek", "--url", "rtsp://127.0.0.1:1/stream", "--timeout", "1s", "--analyze", "--pretty=false"})
	})
	if !strings.Contains(out, `"describe_ok":false`) || strings.Contains(out, `"analysis"`) {
		t.Fatalf("unreachable host should report the error and no analysis, got %q", out)
	}
}

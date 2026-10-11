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
	if app.Version != appVersion() {
		t.Fatalf("app.Version = %q, want %q", app.Version, appVersion())
	}
	if app.Name != "rtspeek" {
		t.Fatalf("app.Name = %q, want %q", app.Name, "rtspeek")
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

func TestRunAnalyzeBadFlagsAreUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"--analyze", "--analyze-transport", "carrier-pigeon"},
		{"--analyze", "--analyze-duration", "5m"},
	} {
		var code int
		out := captureStdout(t, func() {
			code = run(append([]string{"rtspeek", "--url", "rtsp://127.0.0.1:1/stream", "--pretty=false"}, args...))
		})
		if code != 1 || out != "" {
			t.Errorf("%v: code=%d out=%q, want exit 1 and no JSON", args, code, out)
		}
	}
}

func TestRunInvalidURLExitsOneWithFullSchema(t *testing.T) {
	var code int
	out := captureStdout(t, func() {
		code = run([]string{"rtspeek", "--url", "http://example.com/x", "--pretty=false"})
	})
	if code != 1 {
		t.Errorf("exit code %d, want 1", code)
	}
	for _, k := range []string{`"reachable":false`, `"protocol":"rtsp"`, `"media_count":0`, `"latency_ms"`, `"describe_ok":false`} {
		if !strings.Contains(out, k) {
			t.Errorf("output missing %s: %q", k, out)
		}
	}
}

func TestAppName(t *testing.T) {
	if got := newApp().Name; got != "rtspeek" {
		t.Errorf("app name %q", got)
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

func TestWithCredentials(t *testing.T) {
	t.Setenv("RTSPEEK_PASSWORD", "")
	if err := os.Unsetenv("RTSPEEK_PASSWORD"); err != nil {
		t.Fatal(err)
	}
	got, err := withCredentials("rtsp://h/x", "", "", strings.NewReader(""))
	if err != nil || got != "rtsp://h/x" {
		t.Fatalf("no user: %q %v", got, err)
	}
	t.Setenv("RTSPEEK_PASSWORD", "p@ss")
	got, err = withCredentials("rtsp://h/x", "bob", "", strings.NewReader(""))
	if err != nil || got != "rtsp://bob:p%40ss@h/x" {
		t.Fatalf("env password: %q %v", got, err)
	}
	got, err = withCredentials("rtsp://h/x", "bob", "-", strings.NewReader("fromstdin\n"))
	if err != nil || got != "rtsp://bob:fromstdin@h/x" {
		t.Fatalf("stdin password: %q %v", got, err)
	}
	f := t.TempDir() + "/pw"
	if err := os.WriteFile(f, []byte("filepw\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _ = withCredentials("rtsp://h/x", "bob", f, nil); got != "rtsp://bob:filepw@h/x" {
		t.Fatalf("file password: %q", got)
	}
	if _, err = withCredentials("rtsp://u:p@h/x", "bob", "", nil); err == nil {
		t.Error("URL credentials plus --user should fail")
	}
	if _, err = withCredentials("rtsp://h/x", "", f, nil); err == nil {
		t.Error("--password-file without --user should fail")
	}
	if _, err = withCredentials("rtsp://h/x", "bob", "/no/such/file", nil); err == nil {
		t.Error("missing password file should fail")
	}
}

func TestRunFailFlag(t *testing.T) {
	args := []string{"rtspeek", "--url", "rtsp://127.0.0.1:1/x", "--timeout", "1s", "--pretty=false"}
	var code int
	out := captureStdout(t, func() { code = run(args) })
	if code != 0 {
		t.Errorf("without --fail: exit %d, want 0", code)
	}
	for _, k := range []string{`"error_code":"connection_refused"`, `"failed_stage":"connect"`} {
		if !strings.Contains(out, k) {
			t.Errorf("missing %s in %q", k, out)
		}
	}
	captureStdout(t, func() { code = run(append(args, "--fail")) })
	if code != 1 {
		t.Errorf("with --fail: exit %d, want 1", code)
	}
}

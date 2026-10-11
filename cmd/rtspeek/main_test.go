package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"runtime/debug"
	"strings"
	"testing"

	rtpeek "github.com/0x524A/rtspeek/pkg/rtspeek"
	"github.com/bluenviron/gortsplib/v5/pkg/liberrors"
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

func TestVersionFrom(t *testing.T) {
	mod := &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}
	devel := &debug.BuildInfo{Main: debug.Module{Version: "(devel)"},
		Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "0123456789abcdef"}}}
	bare := &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}
	cases := []struct {
		ld   string
		bi   *debug.BuildInfo
		ok   bool
		want string
	}{
		{"v0.9.0", mod, true, "v0.9.0"},
		{"dev", nil, false, "dev"},
		{"dev", mod, true, "v1.2.3"},
		{"dev", devel, true, "dev-0123456"},
		{"dev", bare, true, "dev"},
	}
	for _, c := range cases {
		if got := versionFrom(c.ld, c.bi, c.ok); got != c.want {
			t.Errorf("versionFrom(%q) = %q, want %q", c.ld, got, c.want)
		}
	}
}

func TestWithCredentialsEdgeCases(t *testing.T) {
	if err := os.Unsetenv("RTSPEEK_PASSWORD"); err != nil {
		t.Fatal(err)
	}
	got, err := withCredentials("rtsp://h/x", "bob", "", nil)
	if err != nil || got != "rtsp://bob@h/x" {
		t.Errorf("user only: %q %v", got, err)
	}
	if got, err = withCredentials("rtsp://h:bad port/x", "bob", "", nil); err != nil || got != "rtsp://h:bad port/x" {
		t.Errorf("unparsable URL should pass through: %q %v", got, err)
	}
}

func TestRunMissingPasswordFileIsUsageError(t *testing.T) {
	var code int
	captureStdout(t, func() {
		code = run([]string{"rtspeek", "--url", "rtsp://h/x", "--user", "bob", "--password-file", "/no/such/file"})
	})
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
}

// closedStdout runs fn with os.Stdout replaced by a pipe whose reader is closed,
// so every write fails.
func closedStdout(t *testing.T, fn func()) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old; _ = w.Close() }()
	fn()
}

func TestRunOutputWriteFailures(t *testing.T) {
	// A server that accepts and hangs up gives a partial result (info + error).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	hangup := "rtsp://" + ln.Addr().String() + "/x"
	for _, url := range []string{hangup, "rtsp://127.0.0.1:1/x", "http://example.com/x"} {
		var code int
		closedStdout(t, func() { code = run([]string{"rtspeek", "--url", url, "--timeout", "1s"}) })
		if code != 1 {
			t.Errorf("%s: exit %d, want 1 when stdout is broken", url, code)
		}
	}
	var code int
	captureStdout(t, func() { code = run([]string{"rtspeek", "--url", hangup, "--timeout", "1s", "--fail"}) })
	if code != 1 {
		t.Errorf("partial result with --fail: exit %d, want 1", code)
	}
}

func TestFailureFieldsIncludeStatusCode(t *testing.T) {
	out := map[string]any{}
	addFailureFields(out, fmt.Errorf("x: %w", liberrors.ErrClientBadStatusCode{Code: 404, Message: "Not Found"}))
	if out["status_code"] != 404 || out["error_code"] == "" {
		t.Errorf("got %v", out)
	}
}

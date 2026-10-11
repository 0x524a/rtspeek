package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	neturl "net/url"
	"os"
	"runtime/debug"
	"strings"
	"time"

	rtpeek "github.com/0x524A/rtspeek/pkg/rtspeek"
	cli "github.com/urfave/cli/v2"
)

// version is set at build time via -ldflags "-X main.version=..." (see .goreleaser.yaml)
var version = "dev"

// appVersion returns the build version, falling back to the module version and
// VCS revision that "go install" and "go build" embed.
func appVersion() string {
	if version != "dev" {
		return version
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return version
	}
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	for _, s := range bi.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 7 {
			return "dev-" + s.Value[:7]
		}
	}
	return version
}

func main() {
	os.Exit(run(os.Args))
}

// run executes the CLI app and returns a process exit code, kept separate from
// main so it's testable without invoking os.Exit.
func run(args []string) int {
	if err := newApp().Run(args); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	return 0
}

// newApp builds the CLI app definition.
func newApp() *cli.App {
	return &cli.App{
		Name:    "rtspeek",
		Version: appVersion(),
		Usage:   "Inspect an RTSP URL and output stream description JSON",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "url", Usage: "RTSP URL to inspect", Required: true},
			&cli.DurationFlag{Name: "timeout", Usage: "Timeout for describe", Value: 5 * time.Second},
			&cli.BoolFlag{Name: "pretty", Usage: "Pretty-print JSON output", Value: true},
			&cli.BoolFlag{Name: "verbose", Usage: "Include failure reason on stderr"},
			&cli.BoolFlag{Name: "debug", Usage: "Add a debug_trace array (stages and RTSP request/response lines) to the JSON output"},
			&cli.BoolFlag{Name: "analyze", Usage: "Receive the stream and report packet loss, jitter and bitrate (pulls live video and uses a camera session)"},
			&cli.DurationFlag{Name: "analyze-duration", Usage: "How long to receive media with --analyze (max 60s)", Value: rtpeek.DefaultAnalyzeDuration},
			&cli.StringFlag{Name: "analyze-transport", Usage: "Transport for --analyze: auto, udp or tcp", Value: "auto"},
			&cli.BoolFlag{Name: "fail", Usage: "Exit 1 when OPTIONS or DESCRIBE fails (the JSON is still printed)"},
			&cli.StringFlag{Name: "user", Usage: "Username, when the URL has none", EnvVars: []string{"RTSPEEK_USER"}},
			&cli.StringFlag{Name: "password-file", Usage: "Read the password from this file, or from stdin if \"-\" (else $RTSPEEK_PASSWORD)"},
			&cli.StringFlag{Name: "log-level", Usage: "Log level: disabled, error, warn, info, debug, trace", Value: "disabled"},
			&cli.BoolFlag{Name: "log-console", Usage: "Enable pretty console logging to stderr", Value: false},
		},
		Action: func(c *cli.Context) error {
			url, err := withCredentials(c.String("url"), c.String("user"), c.String("password-file"), os.Stdin)
			if err != nil {
				return err
			}
			timeout := c.Duration("timeout")
			pretty := c.Bool("pretty")
			verbose := c.Bool("verbose")
			debug := c.Bool("debug")
			logLevel := c.String("log-level")
			logConsole := c.Bool("log-console")

			// Setup output formatter
			outputFormatter := NewOutputFormatter(os.Stdout, pretty)

			// Setup logging
			var logger *rtpeek.Logger
			if logLevel != "disabled" {
				level := parseLogLevel(logLevel)
				if logConsole {
					logger = rtpeek.NewLogger(level, os.Stderr, true)
				}
			}

			// Setup context - support both new logging and legacy debug
			ctx := context.Background()
			if debug || (logger != nil) {
				ctx = rtpeek.WithDebug(ctx)
			}
			if logger != nil {
				ctx = rtpeek.WithLogger(ctx, logger)
			}

			// Perform RTSP describe operation
			opts := rtpeek.Options{Timeout: timeout}
			if c.Bool("analyze") {
				opts.Analyze = &rtpeek.AnalyzeOptions{
					Duration:  c.Duration("analyze-duration"),
					Transport: rtpeek.AnalyzeTransport(c.String("analyze-transport")),
				}
				if err := opts.Analyze.Validate(); err != nil {
					return err
				}
			}
			fail := c.Bool("fail")
			info, err := rtpeek.DescribeStreamWithOptions(ctx, url, opts)
			if err != nil {
				// Print verbose error information to stderr if requested
				if verbose {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				}

				// For partial results (connection successful but RTSP failed), output the info with error
				if info != nil {
					if werr := outputFormatter.WriteStreamInfo(info, err); werr != nil {
						return werr
					}
					return failIf(fail, err)
				}

				// For complete failures, output error JSON
				if werr := outputFormatter.WriteErrorOutput(url, err); werr != nil {
					return werr
				}
				// A malformed URL is a usage error, like a bad flag.
				if errors.Is(err, rtpeek.ErrInvalidURL) || rtpeek.FailureReason(err) == rtpeek.ReasonUnsupportedScheme {
					return fmt.Errorf("invalid input: %s", rtpeek.RedactText(err.Error()))
				}
				return failIf(fail, err)
			}

			// Write main JSON output to stdout
			if err := outputFormatter.WriteStreamInfo(info, nil); err != nil {
				return fmt.Errorf("output formatting failed: %w", err)
			}

			return nil
		},
	}
}

// parseLogLevel converts string log level to LogLevel enum
func parseLogLevel(level string) rtpeek.LogLevel {
	switch strings.ToLower(level) {
	case "trace":
		return rtpeek.LogLevelTrace
	case "debug":
		return rtpeek.LogLevelDebug
	case "info":
		return rtpeek.LogLevelInfo
	case "warn", "warning":
		return rtpeek.LogLevelWarn
	case "error":
		return rtpeek.LogLevelError
	default:
		return rtpeek.LogLevelDisabled
	}
}

// failIf turns a failed probe into a non-zero exit when --fail is set.
func failIf(fail bool, err error) error {
	if !fail {
		return nil
	}
	return fmt.Errorf("probe failed: %s", rtpeek.RedactText(err.Error()))
}

// withCredentials adds --user and the password to rawURL. The password comes
// from passwordFile ("-" reads stdin) or $RTSPEEK_PASSWORD, so it stays out of
// the command line. A URL that already carries credentials is left alone, and
// combining it with --user is an error.
func withCredentials(rawURL, user, passwordFile string, stdin io.Reader) (string, error) {
	pass, havePass := os.LookupEnv("RTSPEEK_PASSWORD")
	if passwordFile != "" {
		var data []byte
		var err error
		if passwordFile == "-" {
			data, err = io.ReadAll(stdin)
		} else {
			data, err = os.ReadFile(passwordFile)
		}
		if err != nil {
			return "", fmt.Errorf("reading password: %w", err)
		}
		pass, havePass = strings.TrimRight(string(data), "\r\n"), true
	}
	if user == "" {
		if passwordFile != "" {
			return "", errors.New("--password-file needs --user")
		}
		return rawURL, nil // $RTSPEEK_PASSWORD alone does nothing
	}
	u, err := neturl.Parse(rawURL)
	if err != nil {
		return rawURL, nil // let the library report the bad URL
	}
	if u.User != nil {
		return "", errors.New("the URL already has credentials; drop --user or remove them from the URL")
	}
	if havePass {
		u.User = neturl.UserPassword(user, pass)
	} else {
		u.User = neturl.User(user)
	}
	return u.String(), nil
}

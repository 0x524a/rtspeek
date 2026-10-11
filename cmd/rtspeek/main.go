package main

import (
	"context"
	"errors"
	"fmt"
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
			&cli.StringFlag{Name: "log-level", Usage: "Log level: disabled, error, warn, info, debug, trace", Value: "disabled"},
			&cli.BoolFlag{Name: "log-console", Usage: "Enable pretty console logging to stderr", Value: false},
		},
		Action: func(c *cli.Context) error {
			url := c.String("url")
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
			info, err := rtpeek.DescribeStreamWithOptions(ctx, url, opts)
			if err != nil {
				// Print verbose error information to stderr if requested
				if verbose {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				}

				// For partial results (connection successful but RTSP failed), output the info with error
				if info != nil {
					return outputFormatter.WriteStreamInfo(info, err)
				}

				// For complete failures, output error JSON
				if werr := outputFormatter.WriteErrorOutput(url, err); werr != nil {
					return werr
				}
				// A malformed URL is a usage error, like a bad flag.
				if errors.Is(err, rtpeek.ErrInvalidURL) || rtpeek.FailureReason(err) == rtpeek.ReasonUnsupportedScheme {
					return fmt.Errorf("invalid input: %s", err.Error())
				}
				return nil
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

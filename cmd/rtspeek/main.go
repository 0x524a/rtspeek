package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	rtpeek "github.com/0x524A/rtspeek/pkg/rtspeek"
	cli "github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:  "rtpeek",
		Usage: "Inspect an RTSP URL and output stream description JSON",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "url", Usage: "RTSP URL to inspect", Required: true},
			&cli.DurationFlag{Name: "timeout", Usage: "Timeout for describe", Value: 5 * time.Second},
			&cli.BoolFlag{Name: "pretty", Usage: "Pretty-print JSON output", Value: true},
			&cli.BoolFlag{Name: "verbose", Usage: "Include failure reason on stderr"},
			&cli.BoolFlag{Name: "debug", Usage: "Enable debug logging (legacy compatibility)"},
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
			info, err := rtpeek.DescribeStream(ctx, url, timeout)
			if err != nil {
				// Print verbose error information to stderr if requested
				if verbose {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				}

				// For partial results (connection successful but RTSP failed), output the info
				if info != nil {
					return outputFormatter.WriteStreamInfo(info)
				}

				// For complete failures, output error JSON
				return outputFormatter.WriteErrorOutput(url, err)
			}

			// Write main JSON output to stdout
			if err := outputFormatter.WriteStreamInfo(info); err != nil {
				return fmt.Errorf("output formatting failed: %w", err)
			}

			return nil
		},
	}
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
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

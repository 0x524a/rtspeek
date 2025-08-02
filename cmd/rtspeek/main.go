package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	rtpeek "github.com/example/rtspeek/pkg/rtspeek"
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
			&cli.BoolFlag{Name: "debug", Usage: "Emit RTSP request/response headers into debug_trace"},
		},
		Action: func(c *cli.Context) error {
			url := c.String("url")
			timeout := c.Duration("timeout")
			ctx := context.Background()
			if c.Bool("debug") {
				ctx = rtpeek.WithDebug(ctx)
			}
			info, err := rtpeek.DescribeStream(ctx, url, timeout)
			if err != nil {
				// For invalid URL just surface minimal JSON with error
				if err == rtpeek.ErrInvalidURL {
					enc := json.NewEncoder(os.Stdout)
					if c.Bool("pretty") {
						enc.SetIndent("", "  ")
					}
					_ = enc.Encode(map[string]any{
						"url":            url,
						"describe_ok":    false,
						"failure_reason": "invalid_url",
						"error_message":  err.Error(),
					})
					return nil
				}
				if err != rtpeek.ErrDescribeFailed {
					return fmt.Errorf("describe error: %w", err)
				}
			}
			if info != nil && c.Bool("verbose") && info.Failure() != "" {
				fmt.Fprintf(os.Stderr, "failure: %s (%s)\n", info.Failure(), info.Error())
			}
			enc := json.NewEncoder(os.Stdout)
			if c.Bool("pretty") {
				enc.SetIndent("", "  ")
			}
			if info == nil {
				return nil // already emitted minimal JSON for invalid URL
			}
			out := map[string]any{
				"url":         info.URLString(),
				"reachable":   info.IsReachable(),
				"protocol":    info.ProtocolName(),
				"describe_ok": info.DescribeSucceeded(),
				"latency":     info.LatencyMs(),
				"media_count": info.MediaTotal(),
			}
			if v := info.Video(); len(v) > 0 {
				out["video_medias"] = v
			}
			if a := info.Audio(); len(a) > 0 {
				out["audio_medias"] = a
			}
			if o := info.Other(); len(o) > 0 {
				out["other_medias"] = o
			}
			if fr := info.Failure(); fr != "" {
				out["failure_reason"] = fr
			}
			if em := info.Error(); em != "" {
				out["error_message"] = em
			}
			if dbg := info.Debug(); len(dbg) > 0 {
				out["debug_trace"] = dbg
			}
			if err := enc.Encode(out); err != nil {
				return fmt.Errorf("encode: %w", err)
			}
			return nil
		},
	}
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

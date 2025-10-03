package main

import (
	"encoding/json"
	"fmt"
	"io"

	rtpeek "github.com/0x524A/rtspeek/pkg/rtspeek"
)

// OutputFormatter handles JSON output formatting for the CLI.
type OutputFormatter struct {
	writer io.Writer
	pretty bool
}

// NewOutputFormatter creates a new output formatter.
func NewOutputFormatter(writer io.Writer, pretty bool) *OutputFormatter {
	return &OutputFormatter{
		writer: writer,
		pretty: pretty,
	}
}

// WriteStreamInfo formats and writes StreamInfo as JSON to the output.
func (of *OutputFormatter) WriteStreamInfo(info rtpeek.StreamInfo) error {
	if info == nil {
		return fmt.Errorf("stream info is nil")
	}

	enc := json.NewEncoder(of.writer)
	if of.pretty {
		enc.SetIndent("", "  ")
	}

	output := of.buildOutput(info)
	return enc.Encode(output)
}

// WriteErrorOutput writes a minimal error JSON response.
func (of *OutputFormatter) WriteErrorOutput(url string, err error) error {
	enc := json.NewEncoder(of.writer)
	if of.pretty {
		enc.SetIndent("", "  ")
	}

	output := map[string]any{
		"url":         url,
		"describe_ok": false,
		"error":       err.Error(),
	}

	return enc.Encode(output)
}

// buildOutput constructs the JSON output structure from StreamInfo.
func (of *OutputFormatter) buildOutput(info rtpeek.StreamInfo) map[string]any {
	output := map[string]any{
		"url":         info.GetURLString(),
		"reachable":   info.IsReachable(),
		"protocol":    info.GetProtocolName(),
		"describe_ok": info.IsDescribeSucceeded(),
		"latency":     info.LatencyMs(),
		"media_count": info.GetMediaCount(),
	}

	// Add media collections if they contain items
	if video := info.GetVideoMedias(); len(video) > 0 {
		output["video_medias"] = video
	}
	if audio := info.GetAudioMedias(); len(audio) > 0 {
		output["audio_medias"] = audio
	}
	if other := info.GetOtherMedias(); len(other) > 0 {
		output["other_medias"] = other
	}

	// Add debug trace if present
	if debug := info.GetDebugData(); len(debug) > 0 {
		output["debug_trace"] = debug
	}

	return output
}

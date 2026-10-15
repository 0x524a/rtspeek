package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	rtpeek "github.com/0x524A/rtspeek/pkg/rtspeek"
)

// stubInfo overrides only the accessors buildOutput reads.
type stubInfo struct {
	rtpeek.StreamInfo
	video, audio, other []rtpeek.MediaInfo
	debug               []string
	analysis            *rtpeek.Analysis
}

func (s stubInfo) GetURLString() string               { return "rtsp://host/stream" }
func (s stubInfo) IsReachable() bool                  { return true }
func (s stubInfo) GetProtocolName() string            { return "rtsp" }
func (s stubInfo) IsDescribeSucceeded() bool          { return true }
func (s stubInfo) LatencyMs() float64                 { return 12.5 }
func (s stubInfo) GetMediaCount() int                 { return len(s.video) + len(s.audio) + len(s.other) }
func (s stubInfo) GetVideoMedias() []rtpeek.MediaInfo { return s.video }
func (s stubInfo) GetAudioMedias() []rtpeek.MediaInfo { return s.audio }
func (s stubInfo) GetOtherMedias() []rtpeek.MediaInfo { return s.other }
func (s stubInfo) GetDebugData() []string             { return s.debug }
func (s stubInfo) GetAnalysis() *rtpeek.Analysis      { return s.analysis }

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("invalid JSON %q: %v", b, err)
	}
	return m
}

func TestWriteStreamInfoNil(t *testing.T) {
	of := NewOutputFormatter(&bytes.Buffer{}, false)
	if err := of.WriteStreamInfo(nil, nil); err == nil {
		t.Fatal("expected error for nil info")
	}
}

func TestWriteStreamInfoFull(t *testing.T) {
	var buf bytes.Buffer
	info := stubInfo{
		video: []rtpeek.MediaInfo{{Index: 0, Type: "video"}},
		audio: []rtpeek.MediaInfo{{Index: 1, Type: "audio"}},
		other: []rtpeek.MediaInfo{{Index: 2, Type: "application"}},
		debug: []string{"--> DESCRIBE"},
	}
	if err := NewOutputFormatter(&buf, false).WriteStreamInfo(info, nil); err != nil {
		t.Fatal(err)
	}
	m := decode(t, buf.Bytes())
	for _, k := range []string{"video_medias", "audio_medias", "other_medias", "debug_trace"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing %q in %v", k, m)
		}
	}
	if m["media_count"].(float64) != 3 || m["url"] != "rtsp://host/stream" {
		t.Errorf("unexpected scalar fields: %v", m)
	}
	if _, ok := m["error"]; ok {
		t.Error("error must be absent when describeErr is nil")
	}
}

func TestWriteStreamInfoOmitsEmptyAndIncludesError(t *testing.T) {
	var buf bytes.Buffer
	if err := NewOutputFormatter(&buf, false).WriteStreamInfo(stubInfo{}, errors.New("boom")); err != nil {
		t.Fatal(err)
	}
	m := decode(t, buf.Bytes())
	for _, k := range []string{"video_medias", "audio_medias", "other_medias", "debug_trace"} {
		if _, ok := m[k]; ok {
			t.Errorf("%q should be omitted when empty", k)
		}
	}
	if m["error"] != "boom" {
		t.Errorf("error = %v", m["error"])
	}
}

func TestPrettyIndents(t *testing.T) {
	var pretty, compact bytes.Buffer
	_ = NewOutputFormatter(&pretty, true).WriteStreamInfo(stubInfo{}, nil)
	_ = NewOutputFormatter(&compact, false).WriteStreamInfo(stubInfo{}, nil)
	if !strings.Contains(pretty.String(), "\n  \"") {
		t.Errorf("pretty output not indented: %q", pretty.String())
	}
	if strings.Contains(compact.String(), "\n  ") {
		t.Errorf("compact output indented: %q", compact.String())
	}
}

func TestWriteErrorOutput(t *testing.T) {
	var buf bytes.Buffer
	if err := NewOutputFormatter(&buf, false).WriteErrorOutput("rtsp://x", errors.New("bad")); err != nil {
		t.Fatal(err)
	}
	m := decode(t, buf.Bytes())
	if m["url"] != "rtsp://x" || m["describe_ok"] != false || m["error"] != "bad" {
		t.Errorf("unexpected output: %v", m)
	}
}

func TestWriteStreamInfoAnalysisOnlyWhenPresent(t *testing.T) {
	var buf bytes.Buffer
	of := NewOutputFormatter(&buf, false)

	if err := of.WriteStreamInfo(stubInfo{}, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := decode(t, buf.Bytes())["analysis"]; ok {
		t.Fatal("analysis must be absent when not requested")
	}

	buf.Reset()
	a := &rtpeek.Analysis{Tracks: []rtpeek.TrackAnalysis{{Index: 0, Packets: 10}}, Findings: []rtpeek.Finding{}}
	if err := of.WriteStreamInfo(stubInfo{analysis: a}, nil); err != nil {
		t.Fatal(err)
	}
	got, ok := decode(t, buf.Bytes())["analysis"].(map[string]any)
	if !ok {
		t.Fatalf("analysis missing: %s", buf.String())
	}
	if f, ok := got["findings"].([]any); !ok || len(f) != 0 {
		t.Fatalf("findings should be an empty array, got %v", got["findings"])
	}
}

func TestWriteStreamInfoFrameStatsOnlyWhenPresent(t *testing.T) {
	var buf bytes.Buffer
	a := &rtpeek.Analysis{
		Tracks: []rtpeek.TrackAnalysis{
			{Index: 0, Type: "video", Packets: 10, Frames: &rtpeek.FrameStats{Frames: 5, FPS: 25, Keyframes: 1, HasParamSets: true}},
			{Index: 1, Type: "audio", Packets: 10},
			{Index: 2, Type: "video", Packets: 10, FramesSkipped: "MJPEG is not supported"},
		},
		Findings: []rtpeek.Finding{},
	}
	if err := NewOutputFormatter(&buf, false).WriteStreamInfo(stubInfo{analysis: a}, nil); err != nil {
		t.Fatal(err)
	}
	tracks := decode(t, buf.Bytes())["analysis"].(map[string]any)["tracks"].([]any)
	video := tracks[0].(map[string]any)
	frames, ok := video["frames"].(map[string]any)
	if !ok || frames["fps"] != float64(25) || frames["has_param_sets"] != true {
		t.Fatalf("video frames missing or wrong: %v", video)
	}
	if _, ok := tracks[1].(map[string]any)["frames"]; ok {
		t.Fatal("audio track must have no frames object")
	}
	skipped := tracks[2].(map[string]any)
	if _, ok := skipped["frames"]; ok || skipped["frames_skipped"] != "MJPEG is not supported" {
		t.Fatalf("skipped track wrong: %v", skipped)
	}
}

func TestOutputServerFields(t *testing.T) {
	var buf bytes.Buffer
	info := rtpeek.NewStreamInfo(rtpeek.StreamInfoParams{
		URL: "rtsp://host/s", Reachable: true, Protocol: "rtsp", DescribeOK: true,
		Server: "Acme/1.2", SupportedMethods: []string{"OPTIONS", "DESCRIBE"},
	})
	if err := NewOutputFormatter(&buf, false).WriteStreamInfo(info, nil); err != nil {
		t.Fatal(err)
	}
	m := decode(t, buf.Bytes())
	if m["server"] != "Acme/1.2" {
		t.Errorf("server = %v", m["server"])
	}
	if got, _ := m["supported_methods"].([]any); len(got) != 2 {
		t.Errorf("supported_methods = %v", m["supported_methods"])
	}

	buf.Reset()
	plain := rtpeek.NewStreamInfo(rtpeek.StreamInfoParams{URL: "rtsp://host/s"})
	if err := NewOutputFormatter(&buf, false).WriteStreamInfo(plain, nil); err != nil {
		t.Fatal(err)
	}
	m = decode(t, buf.Bytes())
	if _, ok := m["server"]; ok {
		t.Error("server must be left out when empty")
	}
	if _, ok := m["supported_methods"]; ok {
		t.Error("supported_methods must be left out when empty")
	}

	// A StreamInfo that does not implement ServerInfo still works.
	buf.Reset()
	if err := NewOutputFormatter(&buf, false).WriteStreamInfo(stubInfo{}, nil); err != nil {
		t.Fatal(err)
	}
}

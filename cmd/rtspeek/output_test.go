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

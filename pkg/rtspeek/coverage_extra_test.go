package rtspeek

import (
	"bytes"
	"context"
	"errors"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
)

func sampleStreamInfo() *streamInfo {
	return &streamInfo{
		URL: "rtsp://h/s", Reachable: true, Protocol: "rtsp", DescribeOK: true, Latency: 3.5,
		MediaCount:  3,
		VideoMedias: []MediaInfo{{Index: 0, Type: "video", Resolution: &Resolution{Width: 1920, Height: 1080}}, {Index: 1, Type: "video"}},
		AudioMedias: []MediaInfo{{Index: 2, Type: "audio"}},
		OtherMedias: []MediaInfo{{Index: 3, Type: "application"}},
		DebugTrace:  []string{"a"},
	}
}

func TestStreamInfoAccessors(t *testing.T) {
	s := sampleStreamInfo()
	if s.GetURLString() != "rtsp://h/s" || !s.IsReachable() || s.GetProtocolName() != "rtsp" ||
		!s.IsDescribeSucceeded() || s.LatencyMs() != 3.5 || s.GetMediaCount() != 3 ||
		len(s.GetDebugData()) != 1 || s.Raw() != nil {
		t.Fatalf("basic accessors wrong: %+v", s)
	}
	if len(s.GetVideoMedias()) != 2 || len(s.GetAudioMedias()) != 1 || len(s.GetOtherMedias()) != 1 {
		t.Fatal("media collections wrong")
	}
	if len(s.GetMedias()) != 4 {
		t.Fatalf("GetMedias = %d, want 4", len(s.GetMedias()))
	}
	if want := []string{"video", "video", "audio", "other"}; !reflect.DeepEqual(s.GetMediaTypes(), want) {
		t.Fatalf("GetMediaTypes = %v, want %v", s.GetMediaTypes(), want)
	}
}

func TestStreamInfoVideoHelpers(t *testing.T) {
	s := sampleStreamInfo()
	if !s.HasVideo() {
		t.Fatal("HasVideo should be true")
	}
	if got := s.GetVideoResolutions(); len(got) != 1 || got[0] != (Resolution{1920, 1080}) {
		t.Fatalf("GetVideoResolutions = %v", got)
	}
	if got := s.GetVideoResolutionStrings(); !reflect.DeepEqual(got, []string{"1920x1080"}) {
		t.Fatalf("GetVideoResolutionStrings = %v", got)
	}
	if s.GetVideoResolutionString() != "1920x1080" {
		t.Fatalf("GetVideoResolutionString = %q", s.GetVideoResolutionString())
	}
	if m := s.GetFirstVideoMedia(); m == nil || m.Index != 0 {
		t.Fatalf("GetFirstVideoMedia = %v", m)
	}
}

func TestStreamInfoNoVideo(t *testing.T) {
	s := &streamInfo{}
	if s.HasVideo() || s.GetFirstVideoMedia() != nil || s.GetVideoResolutionString() != "" {
		t.Fatal("empty info should report no video")
	}
	if len(s.GetVideoResolutions()) != 0 || len(s.GetMedias()) != 0 {
		t.Fatal("empty info should have no resolutions or medias")
	}
}

func TestFreeFunctionHelpers(t *testing.T) {
	var si StreamInfo = sampleStreamInfo()
	if len(GetVideoResolutions(si)) != 1 || len(GetVideoResolutionStrings(si)) != 1 ||
		GetVideoResolutionString(si) != "1920x1080" || len(GetMediaTypes(si)) != 4 ||
		len(GetMedias(si)) != 4 || !HasVideo(si) || GetFirstVideoMedia(si) == nil {
		t.Fatal("free-function helpers disagree with methods")
	}
	if r := FirstVideoResolution(si); r == nil || *r != (Resolution{1920, 1080}) {
		t.Fatalf("FirstVideoResolution = %v", r)
	}
	if FirstVideoResolution(&streamInfo{}) != nil {
		t.Fatal("FirstVideoResolution on empty info should be nil")
	}
}

func TestLoggerLevelMethods(t *testing.T) {
	var out bytes.Buffer
	l := NewLogger(LogLevelDebug, &out, false)
	l.Error(errors.New("kaput"), "failed", map[string]interface{}{"k": "v"})
	l.Info("hello", map[string]interface{}{"n": 1})
	l.Debug("dbg")
	for _, want := range []string{"kaput", "failed", "hello", "dbg", `"k":"v"`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q: %s", want, out.String())
		}
	}
}

func TestLoggerMethodsSilentBelowLevel(t *testing.T) {
	var out bytes.Buffer
	l := NewLogger(LogLevelDisabled, &out, false)
	l.Error(errors.New("x"), "m")
	l.Info("m")
	l.Debug("m")
	l.Stage("s")
	l.RTSPRequest("DESCRIBE", "rtsp://h", nil)
	l.RTSPResponse(200, "OK", nil)
	if out.Len() != 0 || len(l.buffer) != 0 {
		t.Fatalf("disabled logger produced output: %q buffer=%d", out.String(), len(l.buffer))
	}
}

func TestLoggerClear(t *testing.T) {
	l := NewLogger(LogLevelDebug, &bytes.Buffer{}, false)
	l.Stage("start")
	if len(l.buffer) == 0 {
		t.Fatal("expected buffered entry")
	}
	l.Clear()
	if len(l.buffer) != 0 || len(l.GetLegacyTrace()) != 0 {
		t.Fatal("Clear did not empty the buffer")
	}
}

func TestLoggerFromContext(t *testing.T) {
	l := NewLogger(LogLevelDebug, &bytes.Buffer{}, false)
	if got := LoggerFromContext(WithLogger(context.Background(), l)); got != l {
		t.Fatal("did not return the stored logger")
	}
	fallback := LoggerFromContext(context.Background())
	if fallback == nil || fallback.level != LogLevelDisabled {
		t.Fatalf("expected disabled fallback logger, got %+v", fallback)
	}
}

func TestCheckReachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()

	ok, err := CheckReachable(context.Background(), "rtsp://"+ln.Addr().String()+"/s", 2*time.Second)
	if err != nil || !ok {
		t.Fatalf("listening port: ok=%v err=%v", ok, err)
	}
	ok, err = CheckReachable(context.Background(), "rtsp://127.0.0.1:1/s", 2*time.Second)
	if ok || err == nil {
		t.Fatalf("closed port: ok=%v err=%v", ok, err)
	}
}

func TestNewLoggerAllLevels(t *testing.T) {
	for _, lvl := range []LogLevel{LogLevelTrace, LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError, LogLevelDisabled} {
		var out bytes.Buffer
		l := NewLogger(lvl, &out, false)
		l.Error(errors.New("e"), "m")
		wantOutput := lvl != LogLevelDisabled
		if (out.Len() > 0) != wantOutput {
			t.Errorf("level %v: output=%v, want %v", lvl, out.Len() > 0, wantOutput)
		}
	}
}

func TestNetworkOperationLevels(t *testing.T) {
	// Success is logged at Debug and buffered.
	var out bytes.Buffer
	l := NewLogger(LogLevelDebug, &out, false)
	l.NetworkOperation("dial", "h:554", time.Millisecond, nil)
	if out.Len() == 0 || len(l.buffer) != 1 || l.buffer[0].Level != LogLevelDebug {
		t.Fatalf("success not logged at debug: out=%q buf=%v", out.String(), l.buffer)
	}

	// Failure is logged at Warn, with the error field, even when Debug is off.
	out.Reset()
	l = NewLogger(LogLevelWarn, &out, false)
	l.NetworkOperation("dial", "h:554", time.Millisecond, errors.New("refused"))
	if !strings.Contains(out.String(), "refused") || len(l.buffer) != 1 || l.buffer[0].Fields["error"] != "refused" {
		t.Fatalf("failure not logged: out=%q buf=%v", out.String(), l.buffer)
	}

	// Success is suppressed at Warn.
	out.Reset()
	l.Clear()
	l.NetworkOperation("dial", "h:554", time.Millisecond, nil)
	if out.Len() != 0 || len(l.buffer) != 0 {
		t.Fatal("success should be suppressed at warn level")
	}
}

func testSession() *description.Session {
	return &description.Session{Medias: []*description.Media{
		{Type: description.MediaTypeVideo, Formats: []format.Format{&format.H264{PayloadTyp: 96}}},
		{Type: description.MediaTypeAudio, Formats: []format.Format{&format.Generic{PayloadTyp: 97, RTPMa: "opus/48000/2"}}},
		{Type: description.MediaTypeApplication, Formats: []format.Format{&format.Generic{PayloadTyp: 98, RTPMa: "x/1000"}}},
	}}
}

func TestProcessMedias(t *testing.T) {
	mp := NewMediaProcessor()

	if err := mp.ProcessMedias(nil, &streamInfo{}); err != nil {
		t.Fatalf("nil description: %v", err)
	}

	info := &streamInfo{}
	if err := mp.ProcessMedias(testSession(), info); err != nil {
		t.Fatal(err)
	}
	if info.MediaCount != 3 || len(info.VideoMedias) != 1 || len(info.AudioMedias) != 1 || len(info.OtherMedias) != 1 {
		t.Fatalf("unexpected classification: %+v", info)
	}

	bad := &description.Session{Medias: []*description.Media{
		{Type: description.MediaTypeVideo, Formats: []format.Format{&format.Generic{PayloadTyp: 96, RTPMa: "MJPEG/90000"}}},
	}}
	if err := mp.ProcessMedias(bad, &streamInfo{}); err == nil {
		t.Fatal("expected error for unsupported video format")
	}
}

func TestProcessMediasWithLogging(t *testing.T) {
	mp := NewMediaProcessor()
	var out bytes.Buffer
	l := NewLogger(LogLevelInfo, &out, false)

	if err := mp.ProcessMediasWithLogging(nil, &streamInfo{}, l); err != nil {
		t.Fatalf("nil description: %v", err)
	}

	info := &streamInfo{}
	if err := mp.ProcessMediasWithLogging(testSession(), info, l); err != nil {
		t.Fatal(err)
	}
	if info.MediaCount != 3 || len(info.VideoMedias) != 1 || len(info.AudioMedias) != 1 || len(info.OtherMedias) != 1 {
		t.Fatalf("unexpected classification: %+v", info)
	}
	if strings.Count(out.String(), "Media processed") != 3 {
		t.Fatalf("expected 3 log lines, got %q", out.String())
	}

	// A nil logger must be tolerated.
	if err := mp.ProcessMediasWithLogging(testSession(), &streamInfo{}, nil); err != nil {
		t.Fatalf("nil logger: %v", err)
	}

	bad := &description.Session{Medias: []*description.Media{
		{Type: description.MediaTypeVideo, Formats: []format.Format{&format.Generic{PayloadTyp: 96, RTPMa: "MJPEG/90000"}}},
	}}
	if err := mp.ProcessMediasWithLogging(bad, &streamInfo{}, l); err == nil {
		t.Fatal("expected error for unsupported video format")
	}
}

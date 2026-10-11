package rtspeek

import (
	"strings"
	"testing"

	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/liberrors"
)

// sps265Main21 is an x265 Main profile, level 2.1 SPS (ffprobe: Main, level 63).
var sps265Main21 = []byte{
	0x42, 0x01, 0x01, 0x01, 0x60, 0x00, 0x00, 0x03, 0x00, 0x90, 0x00, 0x00, 0x03, 0x00, 0x00, 0x03,
	0x00, 0x3f, 0xa0, 0x05, 0x02, 0x01, 0x69, 0x65, 0x95, 0x9a, 0x49, 0x32, 0xbc, 0x05, 0xa0, 0x20,
	0x00, 0x00, 0x03, 0x00, 0x20, 0x00, 0x00, 0x03, 0x03, 0x21,
}

func TestH264Info(t *testing.T) {
	ci := h264Info(sps720p30)
	if ci == nil || ci.Profile != "High" || ci.Level != "3.1" || ci.CodecString != "avc1.64001F" {
		t.Fatalf("got %+v", ci)
	}
	if h264Info([]byte{0x67}) != nil {
		t.Error("garbage SPS should give nil")
	}
}

func TestH265Info(t *testing.T) {
	ci := h265Info(sps265Main21)
	if ci == nil || ci.Profile != "Main (Main tier)" || ci.Level != "2.1" || ci.CodecString != "hvc1.1.6.L63.90" {
		t.Fatalf("got %+v", ci)
	}
	if h265Info([]byte{0x42, 0x01}) != nil {
		t.Error("garbage SPS should give nil")
	}
}

func TestClassifyMediaReportsProfileAndLevel(t *testing.T) {
	m := &MediaInfo{}
	m.setCodecInfo(h264Info(sps720p30))
	if m.CodecString != "avc1.64001F" || m.Profile != "High" || m.Level != "3.1" {
		t.Fatalf("got %+v", m)
	}
	m = &MediaInfo{}
	m.setCodecInfo(nil)
	if m.CodecString != "" {
		t.Error("nil info must leave fields empty")
	}
}

func TestTraceHeaders(t *testing.T) {
	got := traceHeaders(map[string][]string{
		"Server": {"mediamtx"}, "public": {"OPTIONS", "DESCRIBE"}, "Cseq": {"1"},
	})
	want := []string{"    Public: OPTIONS, DESCRIBE", "    Server: mediamtx"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q", got)
	}
	if traceHeaders(nil) != nil {
		t.Error("nil headers should give no lines")
	}
}

func TestFailureDetails(t *testing.T) {
	err := newStageError(StageDescribe, ReasonNotFound, errBadStatus(404))
	if FailedStage(err) != StageDescribe || StatusCode(err) != 404 || FailureReason(err) != ReasonNotFound {
		t.Fatalf("stage=%q code=%d reason=%q", FailedStage(err), StatusCode(err), FailureReason(err))
	}
	if FailureReason(ErrInvalidURL) != ReasonInvalidURL || FailedStage(ErrInvalidURL) != "" || StatusCode(ErrInvalidURL) != 0 {
		t.Error("invalid URL details wrong")
	}
	if newStageError(StageConnect, ReasonOther, nil) != nil {
		t.Error("nil error must stay nil")
	}
}

func errBadStatus(code int) error {
	return liberrors.ErrClientBadStatusCode{Code: base.StatusCode(code), Message: "x"}
}

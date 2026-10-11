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

func TestAnalyzeOptionsValidate(t *testing.T) {
	if err := (AnalyzeOptions{}).Validate(); err != nil {
		t.Errorf("defaults should validate: %v", err)
	}
	if err := (AnalyzeOptions{Transport: "sctp"}).Validate(); err == nil {
		t.Error("unknown transport should fail")
	}
	if err := (AnalyzeOptions{Duration: 2 * MaxAnalyzeDuration}).Validate(); err == nil {
		t.Error("too long duration should fail")
	}
}

// spsBaseline11 is an x264 Constrained Baseline, level 1.1 SPS.
var spsBaseline11 = []byte{
	0x67, 0x42, 0xc0, 0x0b, 0xd9, 0x01, 0x41, 0xfb, 0x01, 0x10, 0x00, 0x00, 0x03, 0x00, 0x10, 0x00,
	0x00, 0x03, 0x03, 0x20, 0xf1, 0x42, 0xa4, 0x80,
}

func TestH264InfoVariants(t *testing.T) {
	cb := h264Info(spsBaseline11)
	if cb == nil || cb.Profile != "Constrained Baseline" || cb.Level != "1.1" || cb.CodecString != "avc1.42C00B" {
		t.Fatalf("constrained baseline: %+v", cb)
	}
	b1 := append([]byte(nil), spsBaseline11...)
	b1[2] |= 0x10 // constraint_set3 with level_idc 11 means level 1b
	if ci := h264Info(b1); ci == nil || ci.Level != "1b" {
		t.Fatalf("level 1b: %+v", ci)
	}
	b2 := append([]byte(nil), spsBaseline11...)
	b2[3] = 9
	if ci := h264Info(b2); ci == nil || ci.Level != "1b" {
		t.Fatalf("level_idc 9: %+v", ci)
	}
	odd := append([]byte(nil), sps720p30...)
	odd[1] = 118
	if ci := h264Info(odd); ci == nil || ci.Profile != "Profile 118" {
		t.Fatalf("unknown profile: %+v", ci)
	}
}

func TestH265InfoTierSpaceAndConstraints(t *testing.T) {
	// Patch a parsed SPS through the builder: flip the tier bit (byte 3, bit 5)
	// and the profile space (byte 3, bits 7-6) of the real vector.
	sps := append([]byte(nil), sps265Main21...)
	sps[3] |= 0x20 // high tier
	hi := h265Info(sps)
	if hi == nil || !strings.Contains(hi.Profile, "High tier") || !strings.Contains(hi.CodecString, ".H63") {
		t.Fatalf("high tier: %+v", hi)
	}
	sps[3] |= 0x40 // profile space 1 -> "A"
	sp := h265Info(sps)
	if sp == nil || !strings.HasPrefix(sp.CodecString, "hvc1.A") {
		t.Fatalf("profile space: %+v", sp)
	}
}

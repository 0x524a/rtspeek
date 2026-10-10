package rtspeek

import (
	"testing"
	"time"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/pion/rtp"
)

func TestThresholdDefaults(t *testing.T) {
	th := Thresholds{}.withDefaults()
	if th.LossWarnPercent != 0.5 || th.LossErrorPercent != 2 || th.JitterWarnMs != 30 || th.JitterErrorMs != 100 {
		t.Fatalf("unexpected defaults: %+v", th)
	}
	custom := Thresholds{LossWarnPercent: 1}.withDefaults()
	if custom.LossWarnPercent != 1 || custom.LossErrorPercent != 2 {
		t.Fatalf("custom value lost: %+v", custom)
	}
}

func TestEvaluateBoundaries(t *testing.T) {
	cases := []struct {
		name     string
		track    TrackAnalysis
		code     string
		severity string // empty: expect no finding with code
	}{
		{"loss below warn", TrackAnalysis{Packets: 100, LossPercent: 0.49}, FindingPacketLoss, ""},
		{"loss at warn", TrackAnalysis{Packets: 100, LossPercent: 0.5}, FindingPacketLoss, SeverityWarn},
		{"loss at error", TrackAnalysis{Packets: 100, LossPercent: 2}, FindingPacketLoss, SeverityError},
		{"jitter below warn", TrackAnalysis{Packets: 100, JitterMs: 29.9}, FindingHighJitter, ""},
		{"jitter at warn", TrackAnalysis{Packets: 100, JitterMs: 30}, FindingHighJitter, SeverityWarn},
		{"jitter at error", TrackAnalysis{Packets: 100, JitterMs: 100}, FindingHighJitter, SeverityError},
	}
	for _, tc := range cases {
		a := &Analysis{Tracks: []TrackAnalysis{tc.track}}
		f := findingByCode(&Analysis{Findings: evaluate(a, Thresholds{})}, tc.code)
		switch {
		case tc.severity == "" && f != nil:
			t.Errorf("%s: unexpected finding %+v", tc.name, f)
		case tc.severity != "" && (f == nil || f.Severity != tc.severity):
			t.Errorf("%s: want %s finding, got %+v", tc.name, tc.severity, f)
		}
	}
}

func TestEvaluateSilentTrackAmongActiveOnes(t *testing.T) {
	a := &Analysis{Tracks: []TrackAnalysis{{Index: 0, Packets: 50}, {Index: 1, Type: "audio"}}}
	fs := evaluate(a, Thresholds{})
	if len(fs) != 1 || fs[0].Code != FindingNoPackets || fs[0].Track == nil || *fs[0].Track != 1 {
		t.Fatalf("want one per-track no_packets finding, got %+v", fs)
	}
}

func TestEvaluateOrdersBySeverity(t *testing.T) {
	a := &Analysis{
		TransportFallback: true,
		Tracks:            []TrackAnalysis{{Packets: 10, LossPercent: 1}, {Index: 1, Packets: 10, LossPercent: 5}},
	}
	fs := evaluate(a, Thresholds{})
	if len(fs) != 3 || fs[0].Severity != SeverityError || fs[1].Severity != SeverityWarn || fs[2].Severity != SeverityInfo {
		t.Fatalf("unexpected order: %+v", fs)
	}
}

func TestNormalizeOptions(t *testing.T) {
	o, err := AnalyzeOptions{}.normalize()
	if err != nil || o.Duration != DefaultAnalyzeDuration || o.Transport != TransportAuto {
		t.Fatalf("defaults wrong: %+v err=%v", o, err)
	}
	if got := o.firstPacketBudget(); got != DefaultAnalyzeFirstPacketTimeout+udpFallbackWait {
		t.Fatalf("auto budget = %v", got)
	}
	o.Transport = TransportTCP
	if got := o.firstPacketBudget(); got != DefaultAnalyzeFirstPacketTimeout {
		t.Fatalf("tcp budget = %v", got)
	}
}

func TestCollectorJitter(t *testing.T) {
	c := newCollector()
	media := newTestMedia()
	f := media.Formats[0].(*format.H264)

	// Perfectly paced packets: 40ms apart on a 90kHz clock => zero jitter.
	t0 := time.Now()
	for i := 0; i < 20; i++ {
		c.packet(media, f, &rtp.Packet{Header: rtp.Header{Timestamp: uint32(i * 3600)}}, t0.Add(time.Duration(i)*40*time.Millisecond))
	}
	if j := c.jitterMs(media); j > 0.01 {
		t.Fatalf("paced stream jitter = %.3fms, want ~0", j)
	}

	// Alternate +20ms / -20ms arrival error: jitter must become clearly non-zero.
	c2 := newCollector()
	for i := 0; i < 40; i++ {
		off := 20 * time.Millisecond
		if i%2 == 1 {
			off = -off
		}
		c2.packet(media, f, &rtp.Packet{Header: rtp.Header{Timestamp: uint32(i * 3600)}}, t0.Add(time.Duration(i)*40*time.Millisecond+off))
	}
	if j := c2.jitterMs(media); j < 5 {
		t.Fatalf("jittery stream jitter = %.3fms, want > 5", j)
	}
}

func TestCollectorSurvivesTimestampWraparound(t *testing.T) {
	c := newCollector()
	media := newTestMedia()
	f := media.Formats[0].(*format.H264)
	t0 := time.Now()
	ts := uint32(0xFFFFFFFF - 3600*3)
	for i := 0; i < 8; i++ {
		c.packet(media, f, &rtp.Packet{Header: rtp.Header{Timestamp: ts}}, t0.Add(time.Duration(i)*40*time.Millisecond))
		ts += 3600
	}
	if j := c.jitterMs(media); j > 0.01 {
		t.Fatalf("wraparound produced jitter %.3fms", j)
	}
}

func newTestMedia() *description.Media {
	return &description.Media{
		Type:    description.MediaTypeVideo,
		Formats: []format.Format{&format.H264{PayloadTyp: 96, PacketizationMode: 1}},
	}
}

func TestTransportName(t *testing.T) {
	cases := map[gortsplib.Protocol]string{
		gortsplib.ProtocolUDP:          "udp",
		gortsplib.ProtocolTCP:          "tcp",
		gortsplib.ProtocolUDPMulticast: "udp-multicast",
		gortsplib.Protocol(99):         "unknown",
	}
	for p, want := range cases {
		if got := transportName(p); got != want {
			t.Errorf("transportName(%v) = %q, want %q", p, got, want)
		}
	}
}

func TestSortFindingsTieBreakers(t *testing.T) {
	two, one := 2, 1
	fs := []Finding{
		{Severity: SeverityWarn, Code: "b", Track: &two},
		{Severity: SeverityWarn, Code: "z", Track: &one},
		{Severity: SeverityWarn, Code: "a", Track: &one},
		{Severity: SeverityWarn, Code: "m"}, // stream-wide sorts before per-track
		{Severity: SeverityError, Code: "x", Track: &two},
	}
	sortFindings(fs)
	got := ""
	for _, f := range fs {
		got += f.Code
	}
	if got != "xmazb" {
		t.Fatalf("order = %q, want xmazb", got)
	}
}

func TestNormalizeRejectsBadOptions(t *testing.T) {
	if _, err := (AnalyzeOptions{Duration: MaxAnalyzeDuration + time.Second}).normalize(); err == nil {
		t.Error("expected error for duration above the maximum")
	}
	if _, err := (AnalyzeOptions{Transport: "x"}).normalize(); err == nil {
		t.Error("expected error for unknown transport")
	}
}

# Frame-level analysis Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend `--analyze` with per-track frame rate, keyframe interval, timestamp-jump and structural bitstream-error checks for H.264 and H.265.

**Architecture:** A new `frameCollector` (`pkg/rtspeek/frames.go`) is fed from the existing `OnPacketRTPAny` callback. Per H.264/H.265 media it runs gortsplib's `rtph264`/`rtph265` decoders to rebuild access units and keeps counters under a mutex. `buildTracks` snapshots it into `TrackAnalysis.Frames`; `evaluate` (still pure) turns that into findings.

**Tech Stack:** Go, `gortsplib/v5` (`rtph264`, `rtph265`), `mediacommon/v2` (`h264`/`h265` `IsRandomAccess`, `SPS.FPS`), `pion/rtp`.

**Spec:** `docs/superpowers/specs/2026-10-10-frame-analysis-design.md`

## Global Constraints

- H.264 and H.265 video tracks only; audio and other codecs get no `frames` object.
- Analysis stays off by default; findings never change the exit code; a failed analysis never fails DESCRIBE.
- Finding codes are stable once released: `no_keyframe`, `keyframe_interval`, `timestamp_jump`, `broken_frame`, `missing_param_sets`, `low_fps`, `frame_analysis_skipped`.
- `evaluate` stays pure (no network, no clock).
- A zero `Thresholds` field selects its default.
- Fewer than 2 frames: report no fps and no keyframe interval, never 0.
- Interval and fps use RTP time, not arrival time.
- Match surrounding code style: terse doc comments, `round2` for floats, table-driven tests, no new dependencies.

## Review Focus

- B-frame streams: RTP timestamps legitimately step backwards in decode order; small backward steps must not count as `timestamp_jump`. (Task 2 test.)
- Joining a stream mid-fragment (normal over UDP): the first orphan FU fragment must not count as a broken frame. (Task 2 test.)
- One lost fragment makes the decoder emit several errors for the same frame; it must count as one broken frame. (Task 2 test.)
- Malformed payloads (empty, truncated FU-A, bad STAP-A) must never crash the process; the packet callback runs on a gortsplib goroutine where `Analyze`'s `recover` does not reach. (Task 2 test.)
- Stream cut short by `stream_interrupted`: no `no_keyframe` finding. (Task 1 test.)
- Window shorter than a typical GOP: `no_keyframe` is `info`, not `warn`. (Task 1 test.)
- Camera without SDP parameter sets and without VUI timing: no `low_fps` guess, `missing_param_sets` only when a keyframe was seen. (Task 1 and 3 tests.)

---

### Task 1: Data model, thresholds and findings

**Files:**
- Modify: `pkg/rtspeek/analysis.go`
- Test: `pkg/rtspeek/analysis_test.go`
- Modify: `docs/superpowers/specs/2026-10-10-frame-analysis-design.md`

**Interfaces:**
- Produces:
  - `type FrameStats struct { Frames uint64; FPS float64; Keyframes uint64; KeyframeIntervalMs float64; TimestampJumps uint64; BrokenFrames uint64; HasParamSets bool; AdvertisedFPS float64 }` with JSON tags `frames`, `fps,omitempty`, `keyframes`, `keyframe_interval_ms,omitempty`, `timestamp_jumps`, `broken_frames`, `has_param_sets`, `advertised_fps,omitempty`.
  - `TrackAnalysis.Frames *FrameStats` (`json:"frames,omitempty"`) and `TrackAnalysis.FramesSkipped string` (`json:"frames_skipped,omitempty"`).
  - Unexported `Analysis.interrupted bool`.
  - Constants `FindingNoKeyframe`, `FindingKeyframeInterval`, `FindingTimestampJump`, `FindingBrokenFrame`, `FindingMissingParamSets`, `FindingLowFPS`, `FindingFrameAnalysisSkipped`.
  - `Thresholds` fields `KeyframeIntervalWarnMs` (10000), `TimestampJumpFactor` (10), `LowFPSRatio` (0.5), `BrokenFramesWarnPercent` (0.5), `BrokenFramesErrorPercent` (5).
  - Unexported const `noKeyframeWarnWindowMs = 10000`.

- [ ] **Step 1: Write the failing tests**

Append to `pkg/rtspeek/analysis_test.go`:

```go
func frameTrack(fs FrameStats) TrackAnalysis {
	return TrackAnalysis{Packets: 100, Type: "video", Frames: &fs}
}

func TestFrameThresholdDefaults(t *testing.T) {
	th := Thresholds{}.withDefaults()
	if th.KeyframeIntervalWarnMs != 10000 || th.TimestampJumpFactor != 10 || th.LowFPSRatio != 0.5 ||
		th.BrokenFramesWarnPercent != 0.5 || th.BrokenFramesErrorPercent != 5 {
		t.Fatalf("unexpected frame defaults: %+v", th)
	}
}

func TestEvaluateFrameFindings(t *testing.T) {
	cases := []struct {
		name       string
		fs         FrameStats
		durationMs float64
		interrupt  bool
		code       string
		severity   string // empty: expect no finding with code
	}{
		{"interval below warn", FrameStats{Frames: 50, Keyframes: 2, KeyframeIntervalMs: 9999, HasParamSets: true}, 5000, false, FindingKeyframeInterval, ""},
		{"interval at warn", FrameStats{Frames: 50, Keyframes: 2, KeyframeIntervalMs: 10000, HasParamSets: true}, 5000, false, FindingKeyframeInterval, SeverityWarn},
		{"no keyframe short window is info", FrameStats{Frames: 50, HasParamSets: true}, 5000, false, FindingNoKeyframe, SeverityInfo},
		{"no keyframe long window is warn", FrameStats{Frames: 50, HasParamSets: true}, 10000, false, FindingNoKeyframe, SeverityWarn},
		{"no keyframe when interrupted", FrameStats{Frames: 50, HasParamSets: true}, 10000, true, FindingNoKeyframe, ""},
		{"no keyframe when no frames", FrameStats{}, 10000, false, FindingNoKeyframe, ""},
		{"timestamp jumps", FrameStats{Frames: 50, Keyframes: 1, TimestampJumps: 2, HasParamSets: true}, 5000, false, FindingTimestampJump, SeverityWarn},
		{"no jumps", FrameStats{Frames: 50, Keyframes: 1, HasParamSets: true}, 5000, false, FindingTimestampJump, ""},
		{"broken below warn", FrameStats{Frames: 1000, BrokenFrames: 4, Keyframes: 1, HasParamSets: true}, 5000, false, FindingBrokenFrame, ""},
		{"broken at warn", FrameStats{Frames: 995, BrokenFrames: 5, Keyframes: 1, HasParamSets: true}, 5000, false, FindingBrokenFrame, SeverityWarn},
		{"broken at error", FrameStats{Frames: 95, BrokenFrames: 5, Keyframes: 1, HasParamSets: true}, 5000, false, FindingBrokenFrame, SeverityError},
		{"keyframe without params", FrameStats{Frames: 50, Keyframes: 2}, 5000, false, FindingMissingParamSets, SeverityWarn},
		{"no keyframe means no param finding", FrameStats{Frames: 50}, 5000, false, FindingMissingParamSets, ""},
		{"low fps", FrameStats{Frames: 50, FPS: 14.9, AdvertisedFPS: 30, Keyframes: 1, HasParamSets: true}, 5000, false, FindingLowFPS, SeverityWarn},
		{"fps at ratio", FrameStats{Frames: 50, FPS: 15, AdvertisedFPS: 30, Keyframes: 1, HasParamSets: true}, 5000, false, FindingLowFPS, ""},
		{"no advertised fps", FrameStats{Frames: 50, FPS: 1, Keyframes: 1, HasParamSets: true}, 5000, false, FindingLowFPS, ""},
	}
	for _, tc := range cases {
		a := &Analysis{DurationMs: tc.durationMs, interrupted: tc.interrupt, Tracks: []TrackAnalysis{frameTrack(tc.fs)}}
		f := findingByCode(&Analysis{Findings: evaluate(a, Thresholds{})}, tc.code)
		switch {
		case tc.severity == "" && f != nil:
			t.Errorf("%s: unexpected finding %+v", tc.name, f)
		case tc.severity != "" && f == nil:
			t.Errorf("%s: expected %s finding", tc.name, tc.code)
		case tc.severity != "" && f.Severity != tc.severity:
			t.Errorf("%s: severity = %s, want %s", tc.name, f.Severity, tc.severity)
		}
	}
}

func TestEvaluateFramesSkippedAndAbsent(t *testing.T) {
	a := &Analysis{Tracks: []TrackAnalysis{
		{Index: 0, Type: "video", Packets: 10, FramesSkipped: "unsupported codec"},
		{Index: 1, Type: "audio", Packets: 10},
	}}
	got := &Analysis{Findings: evaluate(a, Thresholds{})}
	f := findingByCode(got, FindingFrameAnalysisSkipped)
	if f == nil || f.Severity != SeverityInfo || f.Track == nil || *f.Track != 0 {
		t.Fatalf("expected info skipped finding on track 0, got %+v", f)
	}
	if len(got.Findings) != 1 {
		t.Fatalf("audio track must add no frame findings: %+v", got.Findings)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/rtspeek -run 'FrameThreshold|EvaluateFrame' -v`
Expected: FAIL (compile error: `FrameStats`, `FindingKeyframeInterval` etc. undefined).

- [ ] **Step 3: Implement**

In `pkg/rtspeek/analysis.go`:

1. Add finding constants to the existing block:

```go
	FindingNoKeyframe           = "no_keyframe"
	FindingKeyframeInterval     = "keyframe_interval"
	FindingTimestampJump        = "timestamp_jump"
	FindingBrokenFrame          = "broken_frame"
	FindingMissingParamSets     = "missing_param_sets"
	FindingLowFPS               = "low_fps"
	FindingFrameAnalysisSkipped = "frame_analysis_skipped"
```

2. Add below `udpFallbackWait`'s const block:

```go
// noKeyframeWarnWindowMs is the shortest window in which a missing keyframe
// is a warning. Shorter windows can legitimately miss a long GOP.
const noKeyframeWarnWindowMs = 10000
```

3. Extend `Thresholds`:

```go
	KeyframeIntervalWarnMs   float64 // default 10000
	TimestampJumpFactor      float64 // default 10, times the mean frame duration
	LowFPSRatio              float64 // default 0.5, of the rate the SPS advertises
	BrokenFramesWarnPercent  float64 // default 0.5
	BrokenFramesErrorPercent float64 // default 5
```

and in `withDefaults`:

```go
	if t.KeyframeIntervalWarnMs <= 0 {
		t.KeyframeIntervalWarnMs = 10000
	}
	if t.TimestampJumpFactor <= 0 {
		t.TimestampJumpFactor = 10
	}
	if t.LowFPSRatio <= 0 {
		t.LowFPSRatio = 0.5
	}
	if t.BrokenFramesWarnPercent <= 0 {
		t.BrokenFramesWarnPercent = 0.5
	}
	if t.BrokenFramesErrorPercent <= 0 {
		t.BrokenFramesErrorPercent = 5
	}
```

4. Add to `Analysis` the unexported field `interrupted bool // the stream ended before the window did`.

5. Extend `TrackAnalysis` (after `DecodeErrors`):

```go
	// Frames is set for analyzed H.264 and H.265 tracks only.
	Frames *FrameStats `json:"frames,omitempty"`
	// FramesSkipped says why frame analysis did not run on a video track.
	FramesSkipped string `json:"frames_skipped,omitempty"`
```

and add the type:

```go
// FrameStats holds frame-level measurements for one H.264 or H.265 track.
// FPS and KeyframeIntervalMs come from RTP time and are zero when fewer than
// two frames (or keyframes) were seen.
type FrameStats struct {
	Frames             uint64  `json:"frames"`
	FPS                float64 `json:"fps,omitempty"`
	Keyframes          uint64  `json:"keyframes"`
	KeyframeIntervalMs float64 `json:"keyframe_interval_ms,omitempty"`
	TimestampJumps     uint64  `json:"timestamp_jumps"`
	BrokenFrames       uint64  `json:"broken_frames"`
	HasParamSets       bool    `json:"has_param_sets"`
	// AdvertisedFPS is the rate in the SPS timing info, when present.
	AdvertisedFPS float64 `json:"advertised_fps,omitempty"`
}
```

6. In `evaluate`, inside the per-track loop, add before the `if t.Packets == 0` check:

```go
		if t.FramesSkipped != "" {
			findings = append(findings, Finding{
				Severity: SeverityInfo, Code: FindingFrameAnalysisSkipped, Track: idx,
				Message: fmt.Sprintf("frame analysis skipped on track %d: %s", t.Index, t.FramesSkipped),
			})
		}
```

and after the jitter `switch`, before the loop's closing brace:

```go
		if t.Frames != nil {
			findings = append(findings, frameFindings(a, t, idx, th)...)
		}
```

7. Add the helper:

```go
// frameFindings derives the frame-level findings for one track.
func frameFindings(a *Analysis, t TrackAnalysis, idx *int, th Thresholds) []Finding {
	fs := t.Frames
	var out []Finding
	if fs.Keyframes == 0 && fs.Frames > 0 && !a.interrupted {
		sev := SeverityInfo
		if a.DurationMs >= noKeyframeWarnWindowMs {
			sev = SeverityWarn
		}
		out = append(out, Finding{
			Severity: sev, Code: FindingNoKeyframe, Track: idx,
			Message: fmt.Sprintf("no keyframe arrived on track %d within %.1f s", t.Index, a.DurationMs/1000),
		})
	}
	if fs.Keyframes >= 2 && fs.KeyframeIntervalMs >= th.KeyframeIntervalWarnMs {
		out = append(out, Finding{
			Severity: SeverityWarn, Code: FindingKeyframeInterval, Track: idx, Value: new(fs.KeyframeIntervalMs),
			Message: fmt.Sprintf("keyframes on track %d are %.0f ms apart", t.Index, fs.KeyframeIntervalMs),
		})
	}
	if fs.TimestampJumps > 0 {
		out = append(out, Finding{
			Severity: SeverityWarn, Code: FindingTimestampJump, Track: idx, Value: new(float64(fs.TimestampJumps)),
			Message: fmt.Sprintf("%d RTP timestamp jump(s) on track %d", fs.TimestampJumps, t.Index),
		})
	}
	if fs.BrokenFrames > 0 {
		pct := float64(fs.BrokenFrames) / float64(fs.Frames+fs.BrokenFrames) * 100
		sev := ""
		switch {
		case pct >= th.BrokenFramesErrorPercent:
			sev = SeverityError
		case pct >= th.BrokenFramesWarnPercent:
			sev = SeverityWarn
		}
		if sev != "" {
			out = append(out, Finding{
				Severity: sev, Code: FindingBrokenFrame, Track: idx, Value: new(round2(pct)),
				Message: fmt.Sprintf("%d broken frame(s) (%.2f%%) on track %d", fs.BrokenFrames, pct, t.Index),
			})
		}
	}
	if fs.Keyframes > 0 && !fs.HasParamSets {
		out = append(out, Finding{
			Severity: SeverityWarn, Code: FindingMissingParamSets, Track: idx,
			Message: fmt.Sprintf("track %d sent a keyframe but no SPS/PPS, in the SDP or in-band", t.Index),
		})
	}
	if fs.AdvertisedFPS > 0 && fs.FPS > 0 && fs.FPS < th.LowFPSRatio*fs.AdvertisedFPS {
		out = append(out, Finding{
			Severity: SeverityWarn, Code: FindingLowFPS, Track: idx, Value: new(fs.FPS),
			Message: fmt.Sprintf("track %d delivers %.1f fps but the stream advertises %.1f", t.Index, fs.FPS, fs.AdvertisedFPS),
		})
	}
	return out
}
```

8. Update the spec so it matches (edit `docs/superpowers/specs/2026-10-10-frame-analysis-design.md`):
   - In the output JSON and finding list add `advertised_fps` (omitted when the SPS has no timing info) and `frames_skipped` (string on `TrackAnalysis`, set when a video track is not analyzed).
   - Under "Data flow" step 3: backwards timestamp steps count as a jump only when larger than `TimestampJumpFactor` times the mean frame duration, so B-frame reordering is tolerated; a lost-fragment sequence counts as one `broken_frame` per RTP timestamp.
   - Under "Error handling": the packet callback runs on a gortsplib goroutine, so `frameCollector.packet` carries its own `recover`; a panic marks that track `frames_skipped` rather than crashing.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/rtspeek -run 'Threshold|Evaluate' -v && go vet ./...`
Expected: PASS, vet clean.

- [ ] **Step 5: Commit**

```bash
git add pkg/rtspeek/analysis.go pkg/rtspeek/analysis_test.go docs/superpowers/specs/2026-10-10-frame-analysis-design.md
git commit -m "feat: add frame stats model, thresholds and findings"
```

---

### Task 2: Frame collector

**Files:**
- Create: `pkg/rtspeek/frames.go`
- Test: `pkg/rtspeek/frames_test.go`

**Interfaces:**
- Consumes: `FrameStats`, `Thresholds.TimestampJumpFactor` (Task 1); `round2` (`analysis.go`); `extractFormatName` (`media.go`).
- Produces:
  - `func newFrameCollector(desc *description.Session, th Thresholds) *frameCollector`
  - `func (fc *frameCollector) packet(m *description.Media, f format.Format, p *rtp.Packet)`
  - `func (fc *frameCollector) snapshot(m *description.Media) (stats *FrameStats, skipped string)` — both results empty/nil for non-video media; safe on a nil receiver (returns nil, "").

- [ ] **Step 1: Write the failing tests**

Create `pkg/rtspeek/frames_test.go`:

```go
package rtspeek

import (
	"testing"

	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph264"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph265"
	"github.com/pion/rtp"
)

// sps720p30 is a real SPS whose VUI advertises 30 fps (mediacommon test vector).
var sps720p30 = []byte{
	0x67, 0x64, 0x00, 0x1f, 0xac, 0xd9, 0x40, 0x50,
	0x05, 0xbb, 0x01, 0x6c, 0x80, 0x00, 0x00, 0x03,
	0x00, 0x80, 0x00, 0x00, 0x1e, 0x07, 0x8c, 0x18,
	0xcb,
}

const step25 = 3600 // 90 kHz clock at 25 fps

func nalu(header byte, size int) []byte {
	b := make([]byte, size)
	b[0] = header
	return b
}

func idrAU(size int) [][]byte { return [][]byte{nalu(0x67, 20), nalu(0x68, 4), nalu(0x65, size)} }
func pAU(size int) [][]byte   { return [][]byte{nalu(0x41, size)} }

type h264Stream struct {
	t   *testing.T
	enc *rtph264.Encoder
	fc  *frameCollector
	m   *description.Media
	f   format.Format
}

func newH264Stream(t *testing.T, f *format.H264, maxPayload int) *h264Stream {
	t.Helper()
	seq, ssrc := uint16(100), uint32(1)
	enc := &rtph264.Encoder{PayloadType: 96, SSRC: &ssrc, InitialSequenceNumber: &seq, PayloadMaxSize: maxPayload, PacketizationMode: 1}
	if err := enc.Init(); err != nil {
		t.Fatal(err)
	}
	m := &description.Media{Type: description.MediaTypeVideo, Formats: []format.Format{f}}
	fc := newFrameCollector(&description.Session{Medias: []*description.Media{m}}, Thresholds{})
	return &h264Stream{t: t, enc: enc, fc: fc, m: m, f: f}
}

// packets encodes au and stamps every packet with ts.
func (s *h264Stream) packets(au [][]byte, ts uint32) []*rtp.Packet {
	s.t.Helper()
	pkts, err := s.enc.Encode(au)
	if err != nil {
		s.t.Fatal(err)
	}
	for _, p := range pkts {
		p.Timestamp = ts
	}
	return pkts
}

func (s *h264Stream) feed(pkts ...*rtp.Packet) {
	for _, p := range pkts {
		s.fc.packet(s.m, s.f, p)
	}
}

func (s *h264Stream) frameAt(au [][]byte, ts uint32) { s.feed(s.packets(au, ts)...) }

func (s *h264Stream) stats() *FrameStats {
	s.t.Helper()
	fs, skipped := s.fc.snapshot(s.m)
	if fs == nil {
		s.t.Fatalf("no frame stats, skipped=%q", skipped)
	}
	return fs
}

// run feeds n frames 3600 ticks apart with a keyframe every keyEvery frames.
func (s *h264Stream) run(n, keyEvery int) {
	for i := 0; i < n; i++ {
		au := pAU(50)
		if keyEvery > 0 && i%keyEvery == 0 {
			au = idrAU(200)
		}
		s.frameAt(au, uint32(90000+i*step25))
	}
}

func TestFramesCleanStream(t *testing.T) {
	s := newH264Stream(t, &format.H264{PayloadTyp: 96, PacketizationMode: 1}, 1450)
	s.run(30, 10)
	fs := s.stats()
	if fs.Frames != 30 || fs.Keyframes != 3 || fs.TimestampJumps != 0 || fs.BrokenFrames != 0 {
		t.Fatalf("unexpected stats: %+v", fs)
	}
	if fs.FPS != 25 {
		t.Fatalf("fps = %v, want 25", fs.FPS)
	}
	if fs.KeyframeIntervalMs != 400 {
		t.Fatalf("keyframe interval = %v, want 400", fs.KeyframeIntervalMs)
	}
	if !fs.HasParamSets {
		t.Fatal("in-band SPS/PPS should count")
	}
}

func TestFramesFragmentedFrameCountsOnce(t *testing.T) {
	s := newH264Stream(t, &format.H264{PayloadTyp: 96, PacketizationMode: 1}, 100)
	pkts := s.packets(idrAU(600), 90000)
	if len(pkts) < 4 {
		t.Fatalf("expected a fragmented frame, got %d packets", len(pkts))
	}
	s.feed(pkts...)
	s.frameAt(pAU(50), 90000+step25)
	if fs := s.stats(); fs.Frames != 2 || fs.Keyframes != 1 || fs.BrokenFrames != 0 {
		t.Fatalf("unexpected stats: %+v", fs)
	}
}

func TestFramesDroppedFragmentIsOneBrokenFrame(t *testing.T) {
	s := newH264Stream(t, &format.H264{PayloadTyp: 96, PacketizationMode: 1}, 100)
	s.run(5, 0)
	pkts := s.packets(pAU(600), 90000+5*step25)
	if len(pkts) < 4 {
		t.Fatalf("expected a fragmented frame, got %d packets", len(pkts))
	}
	s.feed(pkts[0])
	s.feed(pkts[2:]...) // packet 1 lost: the decoder errors on 2 and on every later fragment
	s.frameAt(pAU(50), 90000+6*step25)
	fs := s.stats()
	if fs.BrokenFrames != 1 {
		t.Fatalf("broken frames = %d, want 1 (one frame, several decoder errors)", fs.BrokenFrames)
	}
	if fs.Frames != 6 {
		t.Fatalf("frames = %d, want 6 (5 before, 1 after)", fs.Frames)
	}
}

func TestFramesJoiningMidFragmentIsNotBroken(t *testing.T) {
	s := newH264Stream(t, &format.H264{PayloadTyp: 96, PacketizationMode: 1}, 100)
	pkts := s.packets(pAU(600), 90000)
	s.feed(pkts[1:]...) // joined mid-frame: no start fragment
	s.frameAt(pAU(50), 90000+step25)
	s.frameAt(pAU(50), 90000+2*step25)
	if fs := s.stats(); fs.BrokenFrames != 0 || fs.Frames != 2 {
		t.Fatalf("unexpected stats: %+v", fs)
	}
}

func TestFramesTimestampJump(t *testing.T) {
	s := newH264Stream(t, &format.H264{PayloadTyp: 96, PacketizationMode: 1}, 1450)
	s.run(10, 5)
	jump := uint32(90000 + 10*step25 + 3600*1000)
	for i := 0; i < 5; i++ {
		s.frameAt(pAU(50), jump+uint32(i*step25))
	}
	fs := s.stats()
	if fs.TimestampJumps != 1 {
		t.Fatalf("jumps = %d, want 1", fs.TimestampJumps)
	}
	if fs.FPS != 25 {
		t.Fatalf("fps = %v, want 25 (the jump must not distort it)", fs.FPS)
	}
}

func TestFramesBackwardsStepsFromBFramesAreTolerated(t *testing.T) {
	s := newH264Stream(t, &format.H264{PayloadTyp: 96, PacketizationMode: 1}, 1450)
	// Decode order with reordering: PTS steps back by up to 2 frame durations.
	for _, n := range []int{0, 3, 1, 2, 6, 4, 5, 9, 7, 8} {
		s.frameAt(pAU(50), uint32(90000+n*step25))
	}
	if fs := s.stats(); fs.TimestampJumps != 0 {
		t.Fatalf("B-frame reordering counted as %d jumps", fs.TimestampJumps)
	}
}

func TestFramesNoKeyframe(t *testing.T) {
	s := newH264Stream(t, &format.H264{PayloadTyp: 96, PacketizationMode: 1}, 1450)
	s.run(10, 0)
	if fs := s.stats(); fs.Keyframes != 0 || fs.Frames != 10 || fs.KeyframeIntervalMs != 0 {
		t.Fatalf("unexpected stats: %+v", fs)
	}
}

func TestFramesParamSets(t *testing.T) {
	bare := newH264Stream(t, &format.H264{PayloadTyp: 96, PacketizationMode: 1}, 1450)
	bare.frameAt([][]byte{nalu(0x65, 100)}, 90000)
	bare.frameAt(pAU(50), 90000+step25)
	if fs := bare.stats(); fs.Keyframes != 1 || fs.HasParamSets {
		t.Fatalf("keyframe without SPS/PPS: %+v", fs)
	}

	seeded := newH264Stream(t, &format.H264{PayloadTyp: 96, PacketizationMode: 1, SPS: sps720p30, PPS: []byte{0x68, 0xeb, 0xe3, 0xcb}}, 1450)
	seeded.frameAt([][]byte{nalu(0x65, 100)}, 90000)
	seeded.frameAt(pAU(50), 90000+step25)
	fs := seeded.stats()
	if !fs.HasParamSets {
		t.Fatalf("SDP parameter sets should count: %+v", fs)
	}
	if fs.AdvertisedFPS != 30 {
		t.Fatalf("advertised fps = %v, want 30", fs.AdvertisedFPS)
	}
}

func TestFramesTooFewFrames(t *testing.T) {
	s := newH264Stream(t, &format.H264{PayloadTyp: 96, PacketizationMode: 1}, 1450)
	s.frameAt(idrAU(100), 90000)
	fs := s.stats()
	if fs.FPS != 0 || fs.KeyframeIntervalMs != 0 {
		t.Fatalf("one frame must give no fps or interval: %+v", fs)
	}
}

func TestFramesMarkerlessCameraSplitsByTimestamp(t *testing.T) {
	s := newH264Stream(t, &format.H264{PayloadTyp: 96, PacketizationMode: 1}, 1450)
	for i := 0; i < 6; i++ {
		for _, p := range s.packets(pAU(50), uint32(90000+i*step25)) {
			p.Marker = false
			s.feed(p)
		}
	}
	// The last frame is still buffered, so 5 of 6 are complete.
	if fs := s.stats(); fs.Frames != 5 || fs.FPS != 25 {
		t.Fatalf("unexpected stats: %+v", fs)
	}
}

func TestFramesMalformedPayloadsNeverPanic(t *testing.T) {
	payloads := [][]byte{
		{}, {0x1c}, {0x1c, 0x85}, {0x18, 0x00}, {0x18, 0xff, 0xff, 0x01}, {0x1d, 0x00}, {0x00}, {0x65},
	}
	s := newH264Stream(t, &format.H264{PayloadTyp: 96, PacketizationMode: 1}, 1450)
	for i, pl := range payloads {
		s.fc.packet(s.m, s.f, &rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: uint16(i), Timestamp: uint32(i * step25), Marker: true}, Payload: pl})
	}
	fs, skipped := s.fc.snapshot(s.m)
	if fs == nil && skipped == "" {
		t.Fatal("snapshot must return stats or a skip reason")
	}
}

func TestFrameCollectorSkipsUnsupportedAndIgnoresAudio(t *testing.T) {
	video := &description.Media{Type: description.MediaTypeVideo, Formats: []format.Format{&format.MJPEG{}}}
	audio := &description.Media{Type: description.MediaTypeAudio, Formats: []format.Format{&format.G711{MULaw: true}}}
	fc := newFrameCollector(&description.Session{Medias: []*description.Media{video, audio}}, Thresholds{})
	if fs, skipped := fc.snapshot(video); fs != nil || skipped == "" {
		t.Fatalf("MJPEG should be skipped with a reason, got %+v %q", fs, skipped)
	}
	if fs, skipped := fc.snapshot(audio); fs != nil || skipped != "" {
		t.Fatalf("audio should be ignored silently, got %+v %q", fs, skipped)
	}
	var nilFC *frameCollector
	if fs, skipped := nilFC.snapshot(video); fs != nil || skipped != "" {
		t.Fatal("nil collector must be safe")
	}
}

func TestFramesH265(t *testing.T) {
	seq, ssrc := uint16(5), uint32(2)
	enc := &rtph265.Encoder{PayloadType: 96, SSRC: &ssrc, InitialSequenceNumber: &seq, PayloadMaxSize: 100}
	if err := enc.Init(); err != nil {
		t.Fatal(err)
	}
	f := &format.H265{PayloadTyp: 96}
	m := &description.Media{Type: description.MediaTypeVideo, Formats: []format.Format{f}}
	fc := newFrameCollector(&description.Session{Medias: []*description.Media{m}}, Thresholds{})

	// H.265 NAL header is two bytes: type = (b0>>1)&0x3f. VPS 32, SPS 33, PPS 34, IDR_W_RADL 19, TRAIL_R 1.
	h265NAL := func(typ byte, size int) []byte {
		b := make([]byte, size)
		b[0], b[1] = typ<<1, 1
		return b
	}
	for i := 0; i < 20; i++ {
		au := [][]byte{h265NAL(1, 300)}
		if i%10 == 0 {
			au = [][]byte{h265NAL(32, 10), h265NAL(33, 20), h265NAL(34, 4), h265NAL(19, 300)}
		}
		pkts, err := enc.Encode(au)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range pkts {
			p.Timestamp = uint32(90000 + i*step25)
			fc.packet(m, f, p)
		}
	}
	fs, _ := fc.snapshot(m)
	if fs == nil || fs.Frames != 20 || fs.Keyframes != 2 || !fs.HasParamSets || fs.FPS != 25 || fs.BrokenFrames != 0 {
		t.Fatalf("unexpected H.265 stats: %+v", fs)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/rtspeek -run 'Frames|FrameCollector' -v`
Expected: FAIL (compile error: `newFrameCollector` undefined).

- [ ] **Step 3: Implement**

Before writing, confirm two decoder facts the code relies on (read-only):
`G=$(go list -m -f '{{.Dir}}' github.com/bluenviron/gortsplib/v5); sed -n 244,280p $G/pkg/format/rtph264/decoder.go` (H.264 `Decode` splits access units by RTP timestamp as well as by marker) and `sed -n 205,262p $G/pkg/format/rtph265/decoder.go` (H.265 `Decode` returns only on marker). If either differs, adjust `splitsByTS` below.

Create `pkg/rtspeek/frames.go`:

```go
package rtspeek

import (
	"errors"
	"fmt"
	"math"
	"sync"

	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph264"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph265"
	h264conf "github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	h265conf "github.com/bluenviron/mediacommon/v2/pkg/codecs/h265"
	"github.com/pion/rtp"
)

// Parameter-set bits tracked per track.
const (
	psSPS uint8 = 1 << iota
	psPPS
	psVPS
)

// auDecoder is the part of the rtph264 and rtph265 decoders used here.
type auDecoder interface {
	Decode(*rtp.Packet) ([][]byte, error)
}

// frameCollector measures frames on H.264 and H.265 tracks. Packet callbacks
// arrive on gortsplib goroutines, so it is locked.
type frameCollector struct {
	mu      sync.Mutex
	tracks  map[*description.Media]*trackFrames
	skipped map[*description.Media]string
}

// trackFrames is the frame state of one track.
type trackFrames struct {
	format format.Format
	dec    auDecoder
	clock  float64
	h265   bool
	// splitsByTS: the decoder also ends an access unit when the RTP timestamp
	// changes (H.264 does, H.265 relies on the marker bit).
	splitsByTS   bool
	morePackets  error
	nonStarting  error
	jumpFactor   float64
	needParams   uint8
	haveParams   uint8
	advertised   float64
	failed       string
	buffered     bool   // the decoder holds an unfinished access unit
	bufTS        uint32 // timestamp of that access unit
	lastBrokenTS uint32
	haveBroken   bool

	frames, keyframes, jumps, broken uint64

	haveTS       bool
	lastTS       uint32
	pos          int64 // RTP timeline in clock ticks, excluding jumps
	sumAbs       int64 // sum of |delta| over accepted deltas
	nDelta       int64
	firstKeyPos  int64
	lastKeyPos   int64
}

func newFrameCollector(desc *description.Session, th Thresholds) *frameCollector {
	th = th.withDefaults()
	fc := &frameCollector{
		tracks:  make(map[*description.Media]*trackFrames),
		skipped: make(map[*description.Media]string),
	}
	for _, m := range desc.Medias {
		if m.Type != description.MediaTypeVideo || len(m.Formats) == 0 {
			continue
		}
		f := m.Formats[0]
		t := &trackFrames{format: f, clock: float64(f.ClockRate()), jumpFactor: th.TimestampJumpFactor}
		switch ct := f.(type) {
		case *format.H264:
			dec, err := ct.CreateDecoder()
			if err != nil {
				fc.skipped[m] = fmt.Sprintf("cannot create H264 decoder: %v", err)
				continue
			}
			t.dec, t.splitsByTS, t.needParams = dec, true, psSPS|psPPS
			t.morePackets, t.nonStarting = rtph264.ErrMorePacketsNeeded, rtph264.ErrNonStartingPacketAndNoPrevious
			sps, pps := ct.SafeParams()
			t.seedParams(sps, pps, nil)
		case *format.H265:
			dec, err := ct.CreateDecoder()
			if err != nil {
				fc.skipped[m] = fmt.Sprintf("cannot create H265 decoder: %v", err)
				continue
			}
			t.dec, t.h265, t.needParams = dec, true, psSPS|psPPS|psVPS
			t.morePackets, t.nonStarting = rtph265.ErrMorePacketsNeeded, rtph265.ErrNonStartingPacketAndNoPrevious
			t.seedParams(ct.SPS, ct.PPS, ct.VPS)
		default:
			fc.skipped[m] = fmt.Sprintf("%s is not supported (H264 and H265 only)", extractFormatName(f))
			continue
		}
		if t.clock <= 0 {
			fc.skipped[m] = "the format has no clock rate"
			continue
		}
		fc.tracks[m] = t
	}
	return fc
}

// seedParams records parameter sets announced in the SDP.
func (t *trackFrames) seedParams(sps, pps, vps []byte) {
	if len(sps) > 0 {
		t.haveParams |= psSPS
		t.advertised = spsFPS(t.h265, sps)
	}
	if len(pps) > 0 {
		t.haveParams |= psPPS
	}
	if len(vps) > 0 {
		t.haveParams |= psVPS
	}
}

// spsFPS returns the frame rate in the SPS timing info, or 0 when absent.
func spsFPS(h265 bool, sps []byte) float64 {
	var fps float64
	if h265 {
		var s h265conf.SPS
		if s.Unmarshal(sps) != nil {
			return 0
		}
		fps = s.FPS()
	} else {
		var s h264conf.SPS
		if s.Unmarshal(sps) != nil {
			return 0
		}
		fps = s.FPS()
	}
	if math.IsNaN(fps) || math.IsInf(fps, 0) || fps <= 0 || fps > 1000 {
		return 0
	}
	return fps
}

// packet feeds one RTP packet. It never panics: this runs on a gortsplib
// goroutine, outside Analyze's recover.
func (fc *frameCollector) packet(m *description.Media, f format.Format, p *rtp.Packet) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	t := fc.tracks[m]
	if t == nil || t.failed != "" || f != t.format {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			t.failed = fmt.Sprintf("frame analysis failed: %v", r)
		}
	}()
	t.packet(p)
}

func (t *trackFrames) packet(p *rtp.Packet) {
	au, err := t.dec.Decode(p)
	switch {
	case err == nil:
		auTS := p.Timestamp
		if t.splitsByTS && t.buffered && p.Timestamp != t.bufTS {
			// The decoder flushed the previous access unit because this packet
			// started a new one, which is now buffered.
			auTS = t.bufTS
			t.bufTS = p.Timestamp
		} else {
			t.buffered = false
		}
		t.accessUnit(au, auTS)
	case errors.Is(err, t.morePackets):
		if !t.pendingFragment(p) {
			t.buffered, t.bufTS = true, p.Timestamp
		}
	case errors.Is(err, t.nonStarting) && t.frames == 0:
		// Joined mid-fragment: normal for a stream that is already running.
	default:
		// One lost fragment makes the decoder fail on every later fragment of
		// the frame; count the frame once.
		if !t.haveBroken || t.lastBrokenTS != p.Timestamp {
			t.broken++
			t.haveBroken, t.lastBrokenTS = true, p.Timestamp
		}
	}
}

// pendingFragment reports whether p is an H.264 FU-A fragment that does not end
// its NAL unit. The decoder keeps those in a separate fragment buffer, so its
// access-unit buffer (and the timestamp tracked for it) is unchanged.
func (t *trackFrames) pendingFragment(p *rtp.Packet) bool {
	return t.splitsByTS && len(p.Payload) >= 2 && p.Payload[0]&0x1f == 28 && p.Payload[1]&0x40 == 0
}

func (t *trackFrames) accessUnit(au [][]byte, ts uint32) {
	t.frames++
	for _, n := range au {
		bit := t.paramBit(n)
		if bit == 0 {
			continue
		}
		t.haveParams |= bit
		if bit == psSPS && t.advertised == 0 {
			t.advertised = spsFPS(t.h265, n)
		}
	}

	if !t.haveTS {
		t.haveTS = true
	} else {
		delta := int64(int32(ts - t.lastTS))
		abs := delta
		if abs < 0 {
			abs = -abs
		}
		if t.nDelta >= 3 && t.sumAbs > 0 && float64(abs) > t.jumpFactor*float64(t.sumAbs)/float64(t.nDelta) {
			t.jumps++ // the timeline carries on as if the jump never happened
		} else {
			t.pos += delta
			t.sumAbs += abs
			t.nDelta++
		}
	}
	t.lastTS = ts

	var key bool
	if t.h265 {
		key = h265conf.IsRandomAccess(au)
	} else {
		key = h264conf.IsRandomAccess(au)
	}
	if key {
		if t.keyframes == 0 {
			t.firstKeyPos = t.pos
		}
		t.keyframes++
		t.lastKeyPos = t.pos
	}
}

// paramBit classifies a NAL unit as SPS, PPS or VPS, or returns 0.
func (t *trackFrames) paramBit(n []byte) uint8 {
	if len(n) == 0 {
		return 0
	}
	if t.h265 {
		switch (n[0] >> 1) & 0x3f {
		case 32:
			return psVPS
		case 33:
			return psSPS
		case 34:
			return psPPS
		}
		return 0
	}
	switch n[0] & 0x1f {
	case 7:
		return psSPS
	case 8:
		return psPPS
	}
	return 0
}

// snapshot returns the stats for m, or the reason frame analysis did not run.
// Both are empty for media that is not video. A nil collector returns nothing.
func (fc *frameCollector) snapshot(m *description.Media) (*FrameStats, string) {
	if fc == nil {
		return nil, ""
	}
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if reason, ok := fc.skipped[m]; ok {
		return nil, reason
	}
	t := fc.tracks[m]
	if t == nil {
		return nil, ""
	}
	if t.failed != "" {
		return nil, t.failed
	}
	fs := &FrameStats{
		Frames:         t.frames,
		Keyframes:      t.keyframes,
		TimestampJumps: t.jumps,
		BrokenFrames:   t.broken,
		HasParamSets:   t.haveParams&t.needParams == t.needParams,
		AdvertisedFPS:  round2(t.advertised),
	}
	if t.nDelta >= 1 && t.pos > 0 {
		fs.FPS = round2(float64(t.nDelta) * t.clock / float64(t.pos))
	}
	if t.keyframes >= 2 && t.lastKeyPos > t.firstKeyPos {
		fs.KeyframeIntervalMs = round2(float64(t.lastKeyPos-t.firstKeyPos) / float64(t.keyframes-1) / t.clock * 1000)
	}
	return fs, ""
}
```

Run `gofmt -w pkg/rtspeek/frames.go pkg/rtspeek/frames_test.go` (the struct field alignment above is not gofmt-exact).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/rtspeek -run 'Frames|FrameCollector' -race -v && go vet ./...`
Expected: PASS. If `TestFramesDroppedFragmentIsOneBrokenFrame` reports a different `Frames` count, print `fs` and check how many frames completed before the loss; fix the test's expectation only after confirming the collector, not the decoder, explains it. If `TestFramesMalformedPayloadsNeverPanic` panics inside gortsplib, the `recover` in `frameCollector.packet` must catch it; if the panic escapes, the recover is placed wrongly.

- [ ] **Step 5: Commit**

```bash
git add pkg/rtspeek/frames.go pkg/rtspeek/frames_test.go
git commit -m "feat: add H.264/H.265 frame collector"
```

---

### Task 3: Wire into Analyze

**Files:**
- Modify: `pkg/rtspeek/analyze.go` (`Analyze`, `buildTracks`)
- Modify: `pkg/rtspeek/analyze_server_test.go` (payload hook, new tests)

**Interfaces:**
- Consumes: `newFrameCollector`, `frameCollector.packet`, `frameCollector.snapshot` (Task 2); `Analysis.interrupted`, `TrackAnalysis.Frames`, `TrackAnalysis.FramesSkipped` (Task 1).
- Produces: `buildTracks(desc *description.Session, stats *gortsplib.ClientStats, col *collector, fc *frameCollector, durationMs float64) []TrackAnalysis`.

- [ ] **Step 1: Write the failing tests**

In `pkg/rtspeek/analyze_server_test.go`, add a field to `analyzeServer`:

```go
	payload     func(n int) []byte // payload of the n-th packet; nil sends a non-IDR slice
```

and in `OnPlay`'s goroutine, next to `ts := uint32(0)` add `n := 0`, then replace the fixed `Payload:` line:

```go
			pl := []byte{0x41, 0x9a, 0x00, 0x01, 0x02, 0x03}
			if h.payload != nil {
				pl = h.payload(n)
			}
			n++
```

(declare `pl` before the `WritePacketRTP` call and use `Payload: pl`).

Append tests:

```go
func idrEvery(k int) func(int) []byte {
	return func(n int) []byte {
		if n%k == 0 {
			return []byte{0x65, 0x88, 0x84, 0x00}
		}
		return []byte{0x41, 0x9a, 0x00, 0x01, 0x02, 0x03}
	}
}

func TestAnalyzeReportsFrameStats(t *testing.T) {
	url := startAnalyzeServer(t, &analyzeServer{send: steady(200, 0, 0), payload: idrEvery(5)})

	info, err := DescribeStreamWithOptions(context.Background(), url, Options{Timeout: 2 * time.Second, Analyze: analyzeOpts()})
	if err != nil {
		t.Fatal(err)
	}
	a := info.GetAnalysis()
	if a == nil || len(a.Tracks) != 1 {
		t.Fatalf("bad analysis: %+v", a)
	}
	fs := a.Tracks[0].Frames
	if fs == nil {
		t.Fatalf("expected frame stats, skipped=%q", a.Tracks[0].FramesSkipped)
	}
	if fs.Frames < 20 || fs.Keyframes < 2 || fs.BrokenFrames != 0 || fs.TimestampJumps != 0 {
		t.Fatalf("unexpected frame stats: %+v", fs)
	}
	if fs.FPS < 24 || fs.FPS > 26 {
		t.Fatalf("fps = %v, want about 25 (RTP time, not arrival time)", fs.FPS)
	}
	if fs.KeyframeIntervalMs < 190 || fs.KeyframeIntervalMs > 210 {
		t.Fatalf("keyframe interval = %v, want about 200", fs.KeyframeIntervalMs)
	}
	// The test server announces no SPS/PPS and sends none in-band.
	if findingByCode(a, FindingMissingParamSets) == nil {
		t.Fatalf("expected missing_param_sets, got %+v", a.Findings)
	}
}

func TestAnalyzeWithoutKeyframesReportsInfoInShortWindow(t *testing.T) {
	url := startAnalyzeServer(t, &analyzeServer{send: steady(200, 0, 0)})

	info, err := DescribeStreamWithOptions(context.Background(), url, Options{Timeout: 2 * time.Second, Analyze: analyzeOpts()})
	if err != nil {
		t.Fatal(err)
	}
	a := info.GetAnalysis()
	f := findingByCode(a, FindingNoKeyframe)
	if f == nil || f.Severity != SeverityInfo {
		t.Fatalf("expected info no_keyframe in a short window, got %+v", f)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/rtspeek -run 'TestAnalyzeReportsFrameStats|TestAnalyzeWithoutKeyframes' -v`
Expected: FAIL (`Frames` is nil: the collector is not wired in yet).

- [ ] **Step 3: Implement**

In `pkg/rtspeek/analyze.go`:

1. In `Analyze`, replace the collector setup and callback:

```go
	col := newCollector()
	fc := newFrameCollector(desc, o.Thresholds)
	rs.client.OnPacketRTPAny(func(m *description.Media, f format.Format, p *rtp.Packet) {
		col.packet(m, f, p, time.Now())
		fc.packet(m, f, p)
	})
```

2. Before `a.Tracks = buildTracks(...)`, record the interruption and pass `fc`:

```go
	a.interrupted = interrupted != nil
	a.Tracks = buildTracks(desc, stats, col, fc, a.DurationMs)
```

3. Change `buildTracks` to accept `fc *frameCollector` after `col`, and inside the per-media loop, before `tracks = append(tracks, t)`:

```go
		t.Frames, t.FramesSkipped = fc.snapshot(m)
```

4. Fix any existing callers: `grep -rn "buildTracks(" pkg` and pass `nil` for `fc` in tests (the nil receiver is safe).

- [ ] **Step 4: Run the full suite**

Run: `go test ./... -race && go vet ./...`
Expected: PASS. The new integration tests need the 10 ms spacing from `steady`; if `Frames` is low, check `analyzeOpts().Duration` (400 ms gives about 40 packets).

- [ ] **Step 5: Commit**

```bash
git add pkg/rtspeek/analyze.go pkg/rtspeek/analyze_server_test.go
git commit -m "feat: report frame stats and findings in --analyze"
```

---

### Task 4: CLI output check and README

**Files:**
- Modify: `cmd/rtspeek/output_test.go`
- Modify: `README.md` (analyze section, findings table, roadmap)

**Interfaces:**
- Consumes: `rtpeek.FrameStats`, `TrackAnalysis.Frames`, `TrackAnalysis.FramesSkipped` (Task 1). `output.go` passes `*Analysis` through unchanged, so no production change is expected here.

- [ ] **Step 1: Write the failing test**

Append to `cmd/rtspeek/output_test.go`:

```go
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
```

- [ ] **Step 2: Run it**

Run: `go test ./cmd/rtspeek -run FrameStats -v`
Expected: PASS already (the structs carry the JSON tags). If it fails, the output layer is reshaping `Analysis`; fix it there.

- [ ] **Step 3: Update the README**

In `README.md`:

1. In the analyze JSON example, add to the track object after `"decode_errors": 0`:

```json
      "frames": {
        "frames": 120,
        "fps": 24.98,
        "keyframes": 2,
        "keyframe_interval_ms": 2000,
        "timestamp_jumps": 0,
        "broken_frames": 0,
        "has_param_sets": true,
        "advertised_fps": 25
      }
```

2. Add a short paragraph after the findings table: frame analysis runs on H.264 and H.265 tracks, measures fps and keyframe interval from RTP time (so network jitter does not skew them), `frames` is absent on audio, and a video track in another codec gets `frames_skipped` plus an `info` finding. Note that a 5 s window can miss a long GOP, so `no_keyframe` is only a warning for windows of 10 s or more.

3. Add rows to the findings table:

| Code | Severity | When |
|------|----------|------|
| `no_keyframe` | warn (info under 10 s) | No keyframe arrived in the window |
| `keyframe_interval` | warn at 10 s | Keyframes are far apart |
| `timestamp_jump` | warn | RTP timestamp leapt, or stepped back by far more than B-frame reordering |
| `broken_frame` | warn at 0.5%, error at 5% | A frame could not be reassembled, for example a lost fragment |
| `missing_param_sets` | warn | A keyframe arrived with no SPS/PPS in the SDP or in-band |
| `low_fps` | warn below 50% | Measured fps is far below the rate the SPS advertises |
| `frame_analysis_skipped` | info | A video track in an unsupported codec |

4. In the Roadmap table, delete the row "Frame-level analysis: frame rate, keyframe interval, timestamp jumps, bitstream errors", and add a row: `Slice-level bitstream checks (frame_num gaps, missing references) and MJPEG/AV1/VP9 frame analysis | Planned`.

5. In the "Time limit" paragraph nothing changes: frame analysis adds no waiting.

- [ ] **Step 4: Verify everything**

Run: `go build ./... && go vet ./... && go test ./... -race`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/rtspeek/output_test.go README.md
git commit -m "docs: document frame-level analysis"
```

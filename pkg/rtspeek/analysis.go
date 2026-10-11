package rtspeek

import (
	"fmt"
	"sort"
	"time"
)

// AnalyzeTransport selects the RTSP transport used while analyzing a stream.
type AnalyzeTransport string

// Transports accepted by AnalyzeOptions.Transport.
const (
	// TransportAuto tries UDP first and falls back to TCP if no packet arrives.
	TransportAuto AnalyzeTransport = "auto"
	// TransportUDP uses UDP only, so network loss is visible.
	TransportUDP AnalyzeTransport = "udp"
	// TransportTCP uses interleaved TCP only. TCP hides network loss.
	TransportTCP AnalyzeTransport = "tcp"
)

// Analysis limits and defaults.
const (
	DefaultAnalyzeDuration           = 5 * time.Second
	MaxAnalyzeDuration               = 60 * time.Second
	DefaultAnalyzeFirstPacketTimeout = 5 * time.Second

	// udpFallbackWait is how long gortsplib waits for a first UDP packet
	// before it switches to TCP (Client.InitialUDPReadTimeout default).
	udpFallbackWait = 3 * time.Second
)

// noKeyframeWarnWindowMs is the shortest window in which a missing keyframe
// is a warning. Shorter windows can legitimately miss a long GOP.
const noKeyframeWarnWindowMs = 10000

// Finding severities.
const (
	SeverityInfo  = "info"
	SeverityWarn  = "warn"
	SeverityError = "error"
)

// Finding codes. Once released these strings are stable.
const (
	FindingPacketLoss           = "packet_loss"
	FindingHighJitter           = "high_jitter"
	FindingNoPackets            = "no_packets"
	FindingStreamInterrupted    = "stream_interrupted"
	FindingTransportFallback    = "transport_fallback"
	FindingSetupFailed          = "setup_failed"
	FindingPlayFailed           = "play_failed"
	FindingNoKeyframe           = "no_keyframe"
	FindingKeyframeInterval     = "keyframe_interval"
	FindingTimestampJump        = "timestamp_jump"
	FindingBrokenFrame          = "broken_frame"
	FindingMissingParamSets     = "missing_param_sets"
	FindingLowFPS               = "low_fps"
	FindingFrameReordering      = "frame_reordering"
	FindingFrameAnalysisSkipped = "frame_analysis_skipped"
)

// Thresholds decide when a measurement becomes a warning or an error.
// A zero field selects its default.
type Thresholds struct {
	LossWarnPercent  float64 // default 0.5
	LossErrorPercent float64 // default 2
	JitterWarnMs     float64 // default 30
	JitterErrorMs    float64 // default 100

	KeyframeIntervalWarnMs   float64 // default 10000
	TimestampJumpFactor      float64 // default 10, times the mean frame duration
	LowFPSRatio              float64 // default 0.5, of the rate the SPS advertises
	BrokenFramesWarnPercent  float64 // default 0.5
	BrokenFramesErrorPercent float64 // default 5
}

func (t Thresholds) withDefaults() Thresholds {
	if t.LossWarnPercent <= 0 {
		t.LossWarnPercent = 0.5
	}
	if t.LossErrorPercent <= 0 {
		t.LossErrorPercent = 2
	}
	if t.JitterWarnMs <= 0 {
		t.JitterWarnMs = 30
	}
	if t.JitterErrorMs <= 0 {
		t.JitterErrorMs = 100
	}
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
	return t
}

// AnalyzeOptions configures stream analysis. Analysis sets up and plays the
// stream, so it pulls live media and occupies a camera session.
type AnalyzeOptions struct {
	// Duration is how long to receive media once the first packet arrives.
	// Default 5s, maximum 60s.
	Duration time.Duration
	// Transport defaults to TransportAuto.
	Transport AnalyzeTransport
	// FirstPacketTimeout is how long to wait for the first packet after PLAY.
	// Default 5s. In auto mode the 3s UDP fallback wait is added on top.
	FirstPacketTimeout time.Duration
	// Thresholds override the default warning and error levels.
	Thresholds Thresholds
}

// normalize fills defaults and validates o.
func (o AnalyzeOptions) normalize() (AnalyzeOptions, error) {
	if o.Duration <= 0 {
		o.Duration = DefaultAnalyzeDuration
	}
	if o.Duration > MaxAnalyzeDuration {
		return o, fmt.Errorf("analyze duration %v exceeds the maximum of %v", o.Duration, MaxAnalyzeDuration)
	}
	switch o.Transport {
	case "":
		o.Transport = TransportAuto
	case TransportAuto, TransportUDP, TransportTCP:
	default:
		return o, fmt.Errorf("unknown analyze transport %q: use auto, udp or tcp", o.Transport)
	}
	if o.FirstPacketTimeout <= 0 {
		o.FirstPacketTimeout = DefaultAnalyzeFirstPacketTimeout
	}
	o.Thresholds = o.Thresholds.withDefaults()
	return o, nil
}

// firstPacketBudget is the longest wait for the first packet.
func (o AnalyzeOptions) firstPacketBudget() time.Duration {
	if o.Transport == TransportAuto {
		return o.FirstPacketTimeout + udpFallbackWait
	}
	return o.FirstPacketTimeout
}

// Options configures DescribeStreamWithOptions.
type Options struct {
	// Timeout bounds dial, OPTIONS and DESCRIBE.
	Timeout time.Duration
	// Analyze, when non-nil, receives the stream after DESCRIBE and reports
	// network and media problems. Nil keeps the describe-only behavior.
	Analyze *AnalyzeOptions
}

// Analysis is the result of receiving a stream for a short window.
type Analysis struct {
	DurationMs          float64         `json:"duration_ms"`
	RequestedDurationMs float64         `json:"requested_duration_ms"`
	Transport           string          `json:"transport,omitempty"`
	TransportFallback   bool            `json:"transport_fallback,omitempty"`
	Tracks              []TrackAnalysis `json:"tracks"`
	Findings            []Finding       `json:"findings"`
	Error               string          `json:"error,omitempty"`

	interrupted bool // the stream ended before the window did
}

// TrackAnalysis holds the measurements for one track.
type TrackAnalysis struct {
	Index        int     `json:"index"`
	Type         string  `json:"type"`
	Format       string  `json:"format,omitempty"`
	Packets      uint64  `json:"packets"`
	Lost         uint64  `json:"lost"`
	LossPercent  float64 `json:"loss_percent"`
	JitterMs     float64 `json:"jitter_ms"`
	Bytes        uint64  `json:"bytes"`
	BitrateKbps  float64 `json:"bitrate_kbps"`
	RTCPPackets  uint64  `json:"rtcp_packets"`
	DecodeErrors uint64  `json:"decode_errors"`
	// Frames is set for analyzed H.264 and H.265 tracks only.
	Frames *FrameStats `json:"frames,omitempty"`
	// FramesSkipped says why frame analysis did not run on a video track.
	FramesSkipped string `json:"frames_skipped,omitempty"`
}

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
	// ReorderedFrames counts frames whose presentation time is earlier than a
	// frame decoded before them, which means the encoder uses B-frames.
	// MaxReorderDepth is the most frames any one of them overtook (capped at 16).
	ReorderedFrames uint64 `json:"reordered_frames"`
	MaxReorderDepth int    `json:"max_reorder_depth,omitempty"`
	// DeclaredReorderDepth is what the SPS allows (0: no B-frames), when it says.
	DeclaredReorderDepth *int `json:"declared_reorder_depth,omitempty"`
}

// Finding is one problem (or notable event) found during analysis.
type Finding struct {
	Severity string   `json:"severity"`
	Code     string   `json:"code"`
	Track    *int     `json:"track,omitempty"`
	Message  string   `json:"message"`
	Value    *float64 `json:"value,omitempty"`
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}

// evaluate turns measurements into findings. It is pure so thresholds can be
// tested without a network.
func evaluate(a *Analysis, th Thresholds) []Finding {
	th = th.withDefaults()
	findings := make([]Finding, 0)

	allSilent := len(a.Tracks) > 0
	for _, t := range a.Tracks {
		if t.Packets > 0 {
			allSilent = false
		}
	}
	if allSilent && a.Error == "" {
		findings = append(findings, Finding{
			Severity: SeverityError, Code: FindingNoPackets,
			Message: "no RTP packets arrived on any track",
		})
	}

	for _, t := range a.Tracks {
		idx := new(t.Index)
		if t.FramesSkipped != "" {
			findings = append(findings, Finding{
				Severity: SeverityInfo, Code: FindingFrameAnalysisSkipped, Track: idx,
				Message: fmt.Sprintf("frame analysis skipped on track %d: %s", t.Index, t.FramesSkipped),
			})
		}
		if t.Packets == 0 {
			if !allSilent {
				findings = append(findings, Finding{
					Severity: SeverityError, Code: FindingNoPackets, Track: idx,
					Message: fmt.Sprintf("no RTP packets arrived on track %d (%s)", t.Index, t.Type),
				})
			}
			continue
		}
		switch {
		case t.LossPercent >= th.LossErrorPercent:
			findings = append(findings, Finding{
				Severity: SeverityError, Code: FindingPacketLoss, Track: idx, Value: new(t.LossPercent),
				Message: fmt.Sprintf("%.2f%% RTP packet loss on track %d", t.LossPercent, t.Index),
			})
		case t.LossPercent >= th.LossWarnPercent:
			findings = append(findings, Finding{
				Severity: SeverityWarn, Code: FindingPacketLoss, Track: idx, Value: new(t.LossPercent),
				Message: fmt.Sprintf("%.2f%% RTP packet loss on track %d", t.LossPercent, t.Index),
			})
		}
		switch {
		case t.JitterMs >= th.JitterErrorMs:
			findings = append(findings, Finding{
				Severity: SeverityError, Code: FindingHighJitter, Track: idx, Value: new(t.JitterMs),
				Message: fmt.Sprintf("%.1f ms jitter on track %d", t.JitterMs, t.Index),
			})
		case t.JitterMs >= th.JitterWarnMs:
			findings = append(findings, Finding{
				Severity: SeverityWarn, Code: FindingHighJitter, Track: idx, Value: new(t.JitterMs),
				Message: fmt.Sprintf("%.1f ms jitter on track %d", t.JitterMs, t.Index),
			})
		}
		if t.Frames != nil {
			findings = append(findings, frameFindings(a, t, idx, th)...)
		}
	}

	if a.TransportFallback {
		findings = append(findings, Finding{
			Severity: SeverityInfo, Code: FindingTransportFallback,
			Message: "no UDP packets arrived, so the client switched to TCP; network loss is hidden on TCP",
		})
	}

	sortFindings(findings)
	return findings
}

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
	if fs.ReorderedFrames > 0 {
		msg := fmt.Sprintf("track %d reorders frames (B-frames), depth %d", t.Index, fs.MaxReorderDepth)
		if fs.DeclaredReorderDepth != nil {
			msg += fmt.Sprintf(", SPS declares %d", *fs.DeclaredReorderDepth)
		}
		out = append(out, Finding{
			Severity: SeverityInfo, Code: FindingFrameReordering, Track: idx, Value: new(float64(fs.MaxReorderDepth)),
			Message: msg,
		})
	}
	return out
}

var severityRank = map[string]int{SeverityError: 0, SeverityWarn: 1, SeverityInfo: 2}

// sortFindings orders by severity, then stream-wide before per-track, then code.
func sortFindings(f []Finding) {
	sort.SliceStable(f, func(i, j int) bool {
		if severityRank[f[i].Severity] != severityRank[f[j].Severity] {
			return severityRank[f[i].Severity] < severityRank[f[j].Severity]
		}
		ti, tj := -1, -1
		if f[i].Track != nil {
			ti = *f[i].Track
		}
		if f[j].Track != nil {
			tj = *f[j].Track
		}
		if ti != tj {
			return ti < tj
		}
		return f[i].Code < f[j].Code
	})
}

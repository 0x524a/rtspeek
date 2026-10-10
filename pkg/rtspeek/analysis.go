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

// Finding severities.
const (
	SeverityInfo  = "info"
	SeverityWarn  = "warn"
	SeverityError = "error"
)

// Finding codes. Once released these strings are stable.
const (
	FindingPacketLoss        = "packet_loss"
	FindingHighJitter        = "high_jitter"
	FindingNoPackets         = "no_packets"
	FindingStreamInterrupted = "stream_interrupted"
	FindingTransportFallback = "transport_fallback"
	FindingSetupFailed       = "setup_failed"
	FindingPlayFailed        = "play_failed"
)

// Thresholds decide when a measurement becomes a warning or an error.
// A zero field selects its default.
type Thresholds struct {
	LossWarnPercent  float64 // default 0.5
	LossErrorPercent float64 // default 2
	JitterWarnMs     float64 // default 30
	JitterErrorMs    float64 // default 100
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

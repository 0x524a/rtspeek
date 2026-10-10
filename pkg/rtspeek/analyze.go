package rtspeek

import (
	"context"
	"fmt"
	"time"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/pion/rtp"
)

// analysisBudget is the longest an analysis can take: SETUP and PLAY, the wait
// for a first packet, the measurement window and a short teardown.
func analysisBudget(timeout time.Duration, o AnalyzeOptions) time.Duration {
	return timeout + o.firstPacketBudget() + o.Duration + 2*time.Second
}

// Analyze receives the stream described by desc for o.Duration and reports
// what it saw. The client must already have completed DESCRIBE. It always
// returns a non-nil Analysis; failures are recorded in Error and Findings.
// Analyze closes the client when it finishes.
func (rs *RTSPSession) Analyze(ctx context.Context, desc *description.Session, o AnalyzeOptions) (a *Analysis) {
	o, err := o.normalize()
	a = &Analysis{
		RequestedDurationMs: float64(o.Duration) / float64(time.Millisecond),
		Tracks:              []TrackAnalysis{},
		Findings:            []Finding{},
	}
	if err != nil {
		a.Error = err.Error()
		return a
	}
	defer func() {
		if r := recover(); r != nil {
			a.Error = fmt.Sprintf("analysis panicked: %v", r)
		}
		rs.closeWithin(time.Second)
	}()

	if rs.logger != nil {
		rs.logger.Stage("analyze")
	}

	switch o.Transport {
	case TransportUDP:
		p := gortsplib.ProtocolUDP
		rs.client.Protocol = &p
	case TransportTCP:
		p := gortsplib.ProtocolTCP
		rs.client.Protocol = &p
	}

	if err := rs.client.SetupAll(desc.BaseURL, desc.Medias); err != nil {
		a.Error = fmt.Sprintf("SETUP failed: %v", err)
		a.Findings = []Finding{{Severity: SeverityError, Code: FindingSetupFailed, Message: a.Error}}
		return a
	}

	col := newCollector()
	rs.client.OnPacketRTPAny(func(m *description.Media, f format.Format, p *rtp.Packet) {
		col.packet(m, f, p, time.Now())
	})

	if _, err := rs.client.Play(nil); err != nil {
		a.Error = fmt.Sprintf("PLAY failed: %v", err)
		a.Findings = []Finding{{Severity: SeverityError, Code: FindingPlayFailed, Message: a.Error}}
		return a
	}

	waitCh := make(chan error, 1)
	go func() { waitCh <- rs.client.Wait() }()

	var interrupted error
	firstTimer := time.NewTimer(o.firstPacketBudget())
	defer firstTimer.Stop()
	select {
	case <-col.first:
	case <-firstTimer.C:
	case interrupted = <-waitCh:
	case <-ctx.Done():
		a.Error = "analysis cancelled: " + ctx.Err().Error()
	}

	if _, ok := col.firstPacketTime(); ok && interrupted == nil && a.Error == "" {
		window := time.NewTimer(o.Duration)
		defer window.Stop()
		select {
		case <-window.C:
		case interrupted = <-waitCh:
		case <-ctx.Done():
			a.Error = "analysis cancelled: " + ctx.Err().Error()
		}
	}

	// Snapshot before closing: Close resets the library statistics.
	var stats *gortsplib.ClientStats
	if interrupted == nil {
		stats = rs.client.Stats()
		if t := rs.client.Transport(); t != nil && t.Session != nil {
			a.Transport = transportName(t.Session.Protocol)
		}
	}
	if start, ok := col.firstPacketTime(); ok {
		a.DurationMs = float64(time.Since(start)) / float64(time.Millisecond)
	}
	a.TransportFallback = rs.switchedTransport.Load()

	a.Tracks = buildTracks(desc, stats, col, a.DurationMs)
	a.Findings = evaluate(a, o.Thresholds)
	if interrupted != nil {
		a.Error = fmt.Sprintf("stream interrupted: %v", interrupted)
		a.Findings = append(a.Findings, Finding{Severity: SeverityError, Code: FindingStreamInterrupted, Message: a.Error})
		sortFindings(a.Findings)
	}
	return a
}

// closeWithin closes the client but gives up waiting after d, so a stuck
// TEARDOWN cannot hold the caller.
func (rs *RTSPSession) closeWithin(d time.Duration) {
	done := make(chan struct{})
	go func() {
		rs.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(d):
	}
}

func transportName(p gortsplib.Protocol) string {
	switch p {
	case gortsplib.ProtocolUDP:
		return "udp"
	case gortsplib.ProtocolTCP:
		return "tcp"
	case gortsplib.ProtocolUDPMulticast:
		return "udp-multicast"
	}
	return "unknown"
}

// buildTracks combines library statistics with the collector's measurements.
// stats may be nil when the stream was interrupted.
func buildTracks(desc *description.Session, stats *gortsplib.ClientStats, col *collector, durationMs float64) []TrackAnalysis {
	tracks := make([]TrackAnalysis, 0, len(desc.Medias))
	for i, m := range desc.Medias {
		t := TrackAnalysis{Index: i, Type: string(m.Type)}
		if len(m.Formats) > 0 {
			t.Format = extractFormatName(m.Formats[0])
		}
		col.mu.Lock()
		if tc := col.tracks[m]; tc != nil {
			t.Packets = tc.packets
		}
		col.mu.Unlock()
		t.JitterMs = round2(col.jitterMs(m))

		if stats != nil {
			if sm, ok := stats.Session.Medias[m]; ok {
				var received, lost uint64
				for _, fs := range sm.Formats {
					received += fs.InboundRTPPackets
					lost += fs.InboundRTPPacketsLost
				}
				t.Packets = received
				t.Lost = lost
				if total := received + lost; total > 0 {
					t.LossPercent = round2(float64(lost) / float64(total) * 100)
				}
				t.Bytes = sm.InboundBytes
				t.RTCPPackets = sm.InboundRTCPPackets
				t.DecodeErrors = sm.InboundRTPPacketsInError
			}
		}
		if durationMs > 0 {
			t.BitrateKbps = round2(float64(t.Bytes) * 8 / durationMs)
		}
		tracks = append(tracks, t)
	}
	return tracks
}

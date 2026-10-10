package rtspeek

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/pion/rtp"
)

// analyzeServer is a fake RTSP server that serves one H264 track and, on PLAY,
// writes a scripted sequence of RTP packets.
type analyzeServer struct {
	stream *gortsplib.ServerStream
	media  *description.Media

	setupStatus base.StatusCode // zero means OK
	playStatus  base.StatusCode // zero means OK
	srv         *gortsplib.Server
	done        chan struct{} // closed on cleanup so writers stop early
	send        func(write func(seq uint16) bool)

	wg sync.WaitGroup
}

func (h *analyzeServer) OnDescribe(*gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	return &base.Response{StatusCode: base.StatusOK}, h.stream, nil
}

func (h *analyzeServer) OnSetup(*gortsplib.ServerHandlerOnSetupCtx) (*base.Response, *gortsplib.ServerStream, error) {
	if h.setupStatus != 0 {
		return &base.Response{StatusCode: h.setupStatus}, nil, nil
	}
	return &base.Response{StatusCode: base.StatusOK}, h.stream, nil
}

func (h *analyzeServer) OnPlay(ctx *gortsplib.ServerHandlerOnPlayCtx) (*base.Response, error) {
	if h.playStatus != 0 {
		return &base.Response{StatusCode: h.playStatus}, nil
	}
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		// Wait until the session really is in the play state.
		deadline := time.Now().Add(2 * time.Second)
		for ctx.Session.State() != gortsplib.ServerSessionStatePlay {
			if time.Now().After(deadline) {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		if h.send == nil {
			return
		}
		ts := uint32(0)
		h.send(func(seq uint16) bool {
			select {
			case <-h.done:
				return false
			default:
			}
			ts += 3600
			_ = h.stream.WritePacketRTP(h.media, &rtp.Packet{
				Header: rtp.Header{
					Version: 2, PayloadType: 96, SequenceNumber: seq,
					Timestamp: ts, SSRC: 1234, Marker: true,
				},
				Payload: []byte{0x41, 0x9a, 0x00, 0x01, 0x02, 0x03},
			})
			return true
		})
	}()
	return &base.Response{StatusCode: base.StatusOK}, nil
}

func startAnalyzeServer(t *testing.T, h *analyzeServer) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()

	h.media = &description.Media{
		Type: description.MediaTypeVideo,
		Formats: []format.Format{
			&format.H264{PayloadTyp: 96, PacketizationMode: 1},
		},
	}
	h.done = make(chan struct{})
	srv := &gortsplib.Server{RTSPAddress: addr, Handler: h}
	if err := srv.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	h.srv = srv
	h.stream = &gortsplib.ServerStream{Server: srv, Desc: &description.Session{Medias: []*description.Media{h.media}}}
	if err := h.stream.Initialize(); err != nil {
		t.Fatalf("stream init: %v", err)
	}
	t.Cleanup(func() {
		close(h.done)
		srv.Close()
		h.wg.Wait()
	})
	return "rtsp://" + addr + "/test"
}

// steady writes n packets 10ms apart, skipping gap sequence numbers after
// packet skipAfter (when gap > 0).
func steady(n, skipAfter, gap int) func(func(uint16) bool) {
	return func(write func(uint16) bool) {
		seq := uint16(1000)
		for i := 0; i < n; i++ {
			if gap > 0 && i == skipAfter {
				seq += uint16(gap)
			}
			if !write(seq) {
				return
			}
			seq++
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func analyzeOpts() *AnalyzeOptions {
	return &AnalyzeOptions{
		Duration:           400 * time.Millisecond,
		Transport:          TransportTCP,
		FirstPacketTimeout: 500 * time.Millisecond,
	}
}

func findingByCode(a *Analysis, code string) *Finding {
	for i := range a.Findings {
		if a.Findings[i].Code == code {
			return &a.Findings[i]
		}
	}
	return nil
}

func TestAnalyzeHealthyStream(t *testing.T) {
	url := startAnalyzeServer(t, &analyzeServer{send: steady(200, 0, 0)})

	info, err := DescribeStreamWithOptions(context.Background(), url, Options{Timeout: 2 * time.Second, Analyze: analyzeOpts()})
	if err != nil {
		t.Fatalf("DescribeStreamWithOptions: %v", err)
	}
	if !info.IsDescribeSucceeded() {
		t.Fatal("describe should succeed")
	}
	a := info.GetAnalysis()
	if a == nil {
		t.Fatal("expected analysis")
	}
	if a.Error != "" {
		t.Fatalf("unexpected analysis error: %s", a.Error)
	}
	if a.Transport != "tcp" {
		t.Fatalf("transport = %q, want tcp", a.Transport)
	}
	if len(a.Tracks) != 1 {
		t.Fatalf("tracks = %d, want 1", len(a.Tracks))
	}
	tr := a.Tracks[0]
	if tr.Packets < 20 || tr.Lost != 0 || tr.Bytes == 0 || tr.BitrateKbps <= 0 {
		t.Fatalf("unexpected track: %+v", tr)
	}
	if f := findingByCode(a, FindingPacketLoss); f != nil {
		t.Fatalf("unexpected loss finding: %+v", f)
	}
	if a.DurationMs < 300 {
		t.Fatalf("measured window %.0fms shorter than requested", a.DurationMs)
	}
}

func TestAnalyzeCountsPacketLoss(t *testing.T) {
	url := startAnalyzeServer(t, &analyzeServer{send: steady(200, 10, 5)})

	info, err := DescribeStreamWithOptions(context.Background(), url, Options{Timeout: 2 * time.Second, Analyze: analyzeOpts()})
	if err != nil {
		t.Fatalf("DescribeStreamWithOptions: %v", err)
	}
	a := info.GetAnalysis()
	if a == nil || len(a.Tracks) != 1 {
		t.Fatalf("bad analysis: %+v", a)
	}
	if a.Tracks[0].Lost != 5 {
		t.Fatalf("lost = %d, want 5", a.Tracks[0].Lost)
	}
	f := findingByCode(a, FindingPacketLoss)
	if f == nil || f.Severity != SeverityError || f.Track == nil || *f.Track != 0 {
		t.Fatalf("expected an error packet_loss finding on track 0, got %+v", a.Findings)
	}
}

func TestAnalyzeNoPackets(t *testing.T) {
	url := startAnalyzeServer(t, &analyzeServer{})

	start := time.Now()
	info, err := DescribeStreamWithOptions(context.Background(), url, Options{Timeout: 2 * time.Second, Analyze: analyzeOpts()})
	if err != nil {
		t.Fatalf("DescribeStreamWithOptions: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Fatalf("took %v, expected to stop after the first-packet timeout", elapsed)
	}
	a := info.GetAnalysis()
	f := findingByCode(a, FindingNoPackets)
	if f == nil || f.Severity != SeverityError || f.Track != nil {
		t.Fatalf("expected a stream-wide no_packets error, got %+v", a.Findings)
	}
}

func TestAnalyzeSetupRejected(t *testing.T) {
	url := startAnalyzeServer(t, &analyzeServer{setupStatus: base.StatusUnsupportedTransport})

	info, err := DescribeStreamWithOptions(context.Background(), url, Options{Timeout: 2 * time.Second, Analyze: analyzeOpts()})
	if err != nil {
		t.Fatalf("a failed analysis must not fail the describe: %v", err)
	}
	if !info.IsDescribeSucceeded() {
		t.Fatal("describe should still be ok")
	}
	a := info.GetAnalysis()
	if a == nil || findingByCode(a, FindingSetupFailed) == nil || a.Error == "" {
		t.Fatalf("expected setup_failed, got %+v", a)
	}
}

func TestDescribeWithoutAnalyzeHasNoAnalysis(t *testing.T) {
	url := startAnalyzeServer(t, &analyzeServer{})

	info, err := DescribeStream(context.Background(), url, 2*time.Second)
	if err != nil {
		t.Fatalf("DescribeStream: %v", err)
	}
	if info.GetAnalysis() != nil {
		t.Fatal("analysis must be nil unless requested")
	}
}

func TestAnalyzeRejectsBadOptions(t *testing.T) {
	cases := []AnalyzeOptions{
		{Duration: 2 * time.Minute},
		{Transport: "carrier-pigeon"},
	}
	for _, o := range cases {
		o := o
		if _, err := DescribeStreamWithOptions(context.Background(), "rtsp://127.0.0.1:1/x", Options{Timeout: time.Second, Analyze: &o}); err == nil {
			t.Fatalf("expected an error for %+v", o)
		}
	}
}

func TestAnalyzePlayRejected(t *testing.T) {
	url := startAnalyzeServer(t, &analyzeServer{playStatus: base.StatusBadRequest})

	info, err := DescribeStreamWithOptions(context.Background(), url, Options{Timeout: 2 * time.Second, Analyze: analyzeOpts()})
	if err != nil {
		t.Fatalf("a failed analysis must not fail the describe: %v", err)
	}
	a := info.GetAnalysis()
	if a == nil || findingByCode(a, FindingPlayFailed) == nil || a.Error == "" {
		t.Fatalf("expected play_failed, got %+v", a)
	}
}

// A server without UDP ports rejects a UDP-only SETUP, which exercises the
// explicit-UDP transport selection without depending on the network.
func TestAnalyzeUDPOnlyRejectedByTCPOnlyServer(t *testing.T) {
	url := startAnalyzeServer(t, &analyzeServer{})
	o := analyzeOpts()
	o.Transport = TransportUDP

	info, err := DescribeStreamWithOptions(context.Background(), url, Options{Timeout: 2 * time.Second, Analyze: o})
	if err != nil {
		t.Fatalf("DescribeStreamWithOptions: %v", err)
	}
	if a := info.GetAnalysis(); a == nil || findingByCode(a, FindingSetupFailed) == nil {
		t.Fatalf("expected setup_failed for UDP against a TCP-only server, got %+v", a)
	}
}

func TestAnalyzeStreamInterrupted(t *testing.T) {
	h := &analyzeServer{send: steady(500, 0, 0)}
	url := startAnalyzeServer(t, h)

	go func() {
		time.Sleep(600 * time.Millisecond)
		h.srv.Close()
	}()

	o := analyzeOpts()
	o.Duration = 10 * time.Second
	start := time.Now()
	info, err := DescribeStreamWithOptions(context.Background(), url, Options{Timeout: 2 * time.Second, Analyze: o})
	if err != nil {
		t.Fatalf("DescribeStreamWithOptions: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 8*time.Second {
		t.Fatalf("took %v: an interrupted stream must end the analysis early", elapsed)
	}
	a := info.GetAnalysis()
	f := findingByCode(a, FindingStreamInterrupted)
	if f == nil || f.Severity != SeverityError || a.Error == "" {
		t.Fatalf("expected stream_interrupted, got %+v", a)
	}
}

func TestAnalyzeCancelled(t *testing.T) {
	h := &analyzeServer{send: steady(1000, 0, 0)}
	url := startAnalyzeServer(t, h)

	parsed, err := base.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	session := NewRTSPSession(2*time.Second, nil)
	desc, _, err := session.PerformDescribe(context.Background(), parsed)
	if err != nil {
		t.Fatalf("describe: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	o := analyzeOpts()
	o.Duration = 30 * time.Second
	start := time.Now()
	a := session.Analyze(ctx, desc, *o)
	if time.Since(start) > 5*time.Second {
		t.Fatal("Analyze ignored its context")
	}
	if a.Error == "" {
		t.Fatalf("expected a cancellation error, got %+v", a)
	}
}

package rtspeek

import (
	"strings"
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
	fs := s.stats()
	if fs.TimestampJumps != 0 {
		t.Fatalf("B-frame reordering counted as %d jumps", fs.TimestampJumps)
	}
	if fs.ReorderedFrames != 6 || fs.MaxReorderDepth != 1 {
		t.Fatalf("reordered = %d depth = %d, want 6 and 1", fs.ReorderedFrames, fs.MaxReorderDepth)
	}
}

func TestFramesDeepReorderingReportsDepth(t *testing.T) {
	s := newH264Stream(t, &format.H264{PayloadTyp: 96, PacketizationMode: 1}, 1450)
	// Hierarchical B: frame 4 is decoded before 1, 2 and 3.
	for _, n := range []int{0, 4, 2, 1, 3, 8, 6, 5, 7} {
		s.frameAt(pAU(50), uint32(90000+n*step25))
	}
	if fs := s.stats(); fs.MaxReorderDepth != 2 || fs.ReorderedFrames != 6 {
		t.Fatalf("reordered = %d depth = %d, want 6 and 2", fs.ReorderedFrames, fs.MaxReorderDepth)
	}
}

func TestFramesInOrderStreamIsNotReordered(t *testing.T) {
	s := newH264Stream(t, &format.H264{PayloadTyp: 96, PacketizationMode: 1}, 1450)
	s.run(20, 5)
	if fs := s.stats(); fs.ReorderedFrames != 0 || fs.MaxReorderDepth != 0 {
		t.Fatalf("in-order stream reported reordering: %+v", fs)
	}
}

func TestFramesDeclaredReorderDepth(t *testing.T) {
	// The test SPS carries a bitstream restriction with max_num_reorder_frames = 2.
	s := newH264Stream(t, &format.H264{PayloadTyp: 96, PacketizationMode: 1, SPS: sps720p30}, 1450)
	s.run(3, 0)
	if fs := s.stats(); fs.DeclaredReorderDepth == nil || *fs.DeclaredReorderDepth != 2 {
		t.Fatalf("declared = %v, want 2", fs.DeclaredReorderDepth)
	}
	if got := spsReorder(false, []byte{0x67}); got != -1 {
		t.Fatalf("garbage SPS gave %d, want -1", got)
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

type panicDecoder struct{ calls int }

func (d *panicDecoder) Decode(*rtp.Packet) ([][]byte, error) {
	d.calls++
	panic("boom")
}

func TestFramesDecoderPanicIsRecovered(t *testing.T) {
	s := newH264Stream(t, &format.H264{PayloadTyp: 96, PacketizationMode: 1}, 1450)
	dec := &panicDecoder{}
	s.fc.tracks[s.m].dec = dec
	pkt := &rtp.Packet{Header: rtp.Header{Version: 2, Marker: true}, Payload: []byte{0x41, 0}}

	s.fc.packet(s.m, s.f, pkt)
	fs, reason := s.fc.snapshot(s.m)
	if fs != nil || !strings.HasPrefix(reason, "frame analysis failed") {
		t.Fatalf("want nil stats and a failure reason, got %+v %q", fs, reason)
	}

	s.fc.packet(s.m, s.f, pkt) // would deadlock if the mutex were still held
	if dec.calls != 1 {
		t.Fatalf("decoder called %d times, want 1 (track must stay failed)", dec.calls)
	}
	if fs2, reason2 := s.fc.snapshot(s.m); fs2 != nil || reason2 != reason {
		t.Fatalf("reason changed: %+v %q", fs2, reason2)
	}
}

// feedTS feeds one single-packet P frame per timestamp.
func (s *h264Stream) feedTS(tss ...uint32) {
	for _, ts := range tss {
		s.frameAt(pAU(50), ts)
	}
}

func TestFramesSustainedRateDropIsRelearned(t *testing.T) {
	s := newH264Stream(t, &format.H264{PayloadTyp: 96, PacketizationMode: 1}, 1450)
	ts := uint32(90000)
	for i := 0; i < 60; i++ { // 2 s at 30 fps
		s.feedTS(ts)
		ts += 3000
	}
	for i := 0; i < 8; i++ { // 8 s at 1 fps
		s.feedTS(ts)
		ts += 90000
	}
	fs := s.stats()
	if fs.TimestampJumps != 0 {
		t.Fatalf("jumps = %d, want 0", fs.TimestampJumps)
	}
	// 67 intervals over 2 s of 30 fps plus 7 s of 1 fps (the first slow step
	// is still 3000 ticks): 67 / 9 s = 7.44.
	if fs.FPS < 7.3 || fs.FPS > 7.6 {
		t.Fatalf("fps = %v, want about 7.44", fs.FPS)
	}
}

func TestFramesLeapOnLastFrameCounts(t *testing.T) {
	s := newH264Stream(t, &format.H264{PayloadTyp: 96, PacketizationMode: 1}, 1450)
	s.run(10, 0)
	s.feedTS(uint32(90000 + 10*step25 + 3600*1000))
	fs := s.stats()
	if fs.TimestampJumps != 1 || fs.FPS != 25 {
		t.Fatalf("unexpected stats: %+v", fs)
	}
}

func TestFramesAlternatingGarbageCountsJumps(t *testing.T) {
	s := newH264Stream(t, &format.H264{PayloadTyp: 96, PacketizationMode: 1}, 1450)
	s.run(10, 0)
	base := uint32(90000 + 9*step25)
	s.feedTS(base+500000000, base, base+500000000, base)
	fs := s.stats()
	if fs.TimestampJumps != 4 || fs.FPS != 25 {
		t.Fatalf("unexpected stats: %+v", fs)
	}
}

func TestFramesBackwardRunsAreNeverRelearned(t *testing.T) {
	s := newH264Stream(t, &format.H264{PayloadTyp: 96, PacketizationMode: 1}, 1450)
	s.run(10, 0)
	ts := uint32(90000 + 9*step25)
	for i := 0; i < 5; i++ {
		ts -= 1000000
		s.feedTS(ts)
	}
	fs := s.stats()
	if fs.TimestampJumps != 5 || fs.FPS != 25 {
		t.Fatalf("unexpected stats: %+v", fs)
	}
}

func TestFramesLeapOnFirstDeltaDoesNotPoisonBaseline(t *testing.T) {
	s := newH264Stream(t, &format.H264{PayloadTyp: 96, PacketizationMode: 1}, 1450)
	s.feedTS(0, 500000000)
	for i := 1; i <= 10; i++ {
		s.feedTS(uint32(500000000 + i*step25))
	}
	fs := s.stats()
	if fs.TimestampJumps != 1 || fs.FPS < 24.5 || fs.FPS > 25.5 {
		t.Fatalf("unexpected stats: %+v", fs)
	}
}

func TestFramesSameTimestampAUsCountOnce(t *testing.T) {
	s := newH264Stream(t, &format.H264{PayloadTyp: 96, PacketizationMode: 1}, 1450)
	for i := 0; i < 50; i++ {
		ts := uint32(90000 + i*step25)
		first := pAU(50)
		if i%25 == 0 {
			first = idrAU(200)
		}
		s.frameAt(first, ts)
		for k := 0; k < 3; k++ { // each extra slice is its own marker'd access unit
			if i%25 == 0 {
				s.frameAt([][]byte{nalu(0x65, 80)}, ts)
			} else {
				s.frameAt(pAU(80), ts)
			}
		}
	}
	fs := s.stats()
	if fs.Frames != 50 || fs.Keyframes != 2 || fs.FPS != 25 || fs.KeyframeIntervalMs != 1000 || fs.TimestampJumps != 0 {
		t.Fatalf("unexpected stats: %+v", fs)
	}
}

func TestFramesMarkerlessH265IsSkipped(t *testing.T) {
	seq, ssrc := uint16(5), uint32(2)
	enc := &rtph265.Encoder{PayloadType: 96, SSRC: &ssrc, InitialSequenceNumber: &seq, PayloadMaxSize: 100}
	if err := enc.Init(); err != nil {
		t.Fatal(err)
	}
	f := &format.H265{PayloadTyp: 96}
	m := &description.Media{Type: description.MediaTypeVideo, Formats: []format.Format{f}}
	fc := newFrameCollector(&description.Session{Medias: []*description.Media{m}}, Thresholds{})
	for i := 0; i < 5; i++ {
		b := make([]byte, 300)
		b[0], b[1] = 1<<1, 1
		pkts, err := enc.Encode([][]byte{b})
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range pkts {
			p.Timestamp = uint32(90000 + i*step25)
			p.Marker = false
			fc.packet(m, f, p)
		}
	}
	// A malformed payload is a decoder error; no access unit ever completes.
	fc.packet(m, f, &rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: 99, Timestamp: 1}, Payload: []byte{0x00}})
	fs, skipped := fc.snapshot(m)
	if fs != nil || skipped == "" {
		t.Fatalf("want a skip reason, got %+v %q", fs, skipped)
	}
}

func TestFramesInBandSPSOverridesAdvertisedFPS(t *testing.T) {
	s := newH264Stream(t, &format.H264{PayloadTyp: 96, PacketizationMode: 1}, 1450)
	s.fc.tracks[s.m].advertised = 12
	s.frameAt([][]byte{sps720p30, nalu(0x68, 4), nalu(0x65, 100)}, 90000)
	s.frameAt(pAU(50), 90000+step25)
	if fs := s.stats(); fs.AdvertisedFPS != 30 {
		t.Fatalf("advertised fps = %v, want 30", fs.AdvertisedFPS)
	}
}

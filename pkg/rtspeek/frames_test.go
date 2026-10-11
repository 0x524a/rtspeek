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

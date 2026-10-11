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

	haveTS      bool
	lastTS      uint32
	pos         int64 // RTP timeline in clock ticks, excluding jumps
	sumAbs      int64 // sum of |delta| over accepted deltas
	nDelta      int64
	firstKeyPos int64
	lastKeyPos  int64
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

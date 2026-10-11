package rtspeek

import (
	"errors"
	"fmt"
	"math"
	"sort"
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
	baseSum     int64 // sum of |delta| behind the baseline mean
	baseN       int64
	seed        []int64
	seeded      bool
	runN        int64 // pending run of outlier deltas
	runPos      int64
	runFirst    int64
	runKeys     []int64
	keyAtTS     bool
	haveKey     bool
	nDelta      int64
	firstKeyPos int64
	lastKeyPos  int64

	recent       [reorderWindow]int64 // RTP positions of the latest frames
	recentN      int
	reordered    uint64
	reorderDepth int
	declared     int // reorder depth the SPS declares, -1 when unknown
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
		t := &trackFrames{format: f, clock: float64(f.ClockRate()), jumpFactor: th.TimestampJumpFactor, declared: -1}
		switch ct := f.(type) {
		case *format.H264:
			dec, err := ct.CreateDecoder()
			if err != nil {
				fc.skipped[m] = fmt.Sprintf("cannot create H264 decoder: %v", err)
				continue
			}
			t.dec, t.splitsByTS, t.needParams = dec, true, psSPS|psPPS
			t.morePackets, t.nonStarting = rtph264.ErrMorePacketsNeeded, rtph264.ErrNonStartingPacketAndNoPrevious
			t.seedParams(ct.SPS, ct.PPS, nil)
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
		t.declared = spsReorder(t.h265, sps)
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

// spsReorder returns the reorder depth the SPS declares (0 means no B-frames),
// or -1 when it does not say.
func spsReorder(h265 bool, sps []byte) int {
	if h265 {
		var s h265conf.SPS
		if s.Unmarshal(sps) != nil || len(s.MaxNumReorderPics) == 0 {
			return -1
		}
		return int(s.MaxNumReorderPics[len(s.MaxNumReorderPics)-1])
	}
	var s h264conf.SPS
	if s.Unmarshal(sps) != nil {
		return -1
	}
	switch {
	case s.PicOrderCntType == 2:
		return 0
	case s.VUI != nil && s.VUI.BitstreamRestriction != nil:
		return int(s.VUI.BitstreamRestriction.MaxNumReorderFrames)
	}
	return -1
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

// Number of consecutive consistent deltas that make a rate change real, and
// number of initial deltas used to seed the baseline.
const (
	rateChangeRun = 3
	seedDeltas    = 3
	// reorderWindow is how many recent frames are compared to spot reordering.
	reorderWindow = 16
)

func (t *trackFrames) accessUnit(au [][]byte, ts uint32) {
	for _, n := range au {
		bit := t.paramBit(n)
		if bit == 0 {
			continue
		}
		t.haveParams |= bit
		if bit == psSPS {
			// An in-band SPS beats the SDP one, which is often stale.
			if fps := spsFPS(t.h265, n); fps > 0 {
				t.advertised = fps
			}
			if d := spsReorder(t.h265, n); d >= 0 {
				t.declared = d
			}
		}
	}

	var key bool
	if t.h265 {
		key = h265conf.IsRandomAccess(au)
	} else {
		key = h264conf.IsRandomAccess(au)
	}

	// Several access units can share one RTP timestamp (marker bit on every
	// slice, or on a separate SPS/PPS packet). They are one frame.
	if t.haveTS && ts == t.lastTS {
		if key {
			t.noteKey()
		}
		return
	}

	t.frames++
	t.keyAtTS = false
	if !t.haveTS {
		t.haveTS = true
		t.noteOrder(0)
	} else {
		t.delta(int64(int32(ts - t.lastTS)))
	}
	t.lastTS = ts
	if key {
		t.noteKey()
	}
}

func absInt(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// delta folds one timestamp step into the timeline. Steps far above the running
// mean are held in a pending run: a lone one is a timestamp jump, but a run of
// consistent ones is a real rate change (dynamic fps) and is learned.
func (t *trackFrames) delta(d int64) {
	if !t.seeded {
		t.pos += d
		t.seed = append(t.seed, d)
		if len(t.seed) == seedDeltas {
			t.commitSeed()
		}
		return
	}
	abs := absInt(d)
	mean := float64(t.baseSum) / float64(t.baseN)
	if mean <= 0 || float64(abs) <= t.jumpFactor*mean {
		t.flushRun()
		t.pos += d
		t.noteOrder(t.pos)
		t.nDelta++
		t.baseSum += abs
		t.baseN++
		return
	}
	if t.runN > 0 && (t.runFirst <= 0 || d < t.runFirst/2 || d > 2*t.runFirst) {
		t.flushRun()
	}
	if t.runN == 0 {
		t.runFirst = d
	}
	t.runN++
	t.runPos += d
	if t.runN >= rateChangeRun && t.runFirst > 0 {
		base := t.pos
		t.pos += t.runPos
		t.noteOrder(t.pos)
		t.nDelta += t.runN
		t.baseSum, t.baseN = t.runPos, t.runN
		for _, rel := range t.runKeys {
			t.recordKey(base + rel)
		}
		t.resetRun()
	}
}

// commitSeed judges the first deltas against their median, so a leap among
// them cannot poison the baseline.
func (t *trackFrames) commitSeed() {
	abs := make([]int64, len(t.seed))
	for i, d := range t.seed {
		abs[i] = absInt(d)
	}
	sort.Slice(abs, func(i, j int) bool { return abs[i] < abs[j] })
	med := float64(abs[len(abs)/2])
	var p int64
	for _, d := range t.seed {
		if float64(absInt(d)) > t.jumpFactor*med {
			t.jumps++
			t.pos -= d
			continue
		}
		p += d
		t.noteOrder(p)
		t.nDelta++
		t.baseSum += absInt(d)
		t.baseN++
	}
	t.seed, t.seeded = nil, true
}

// noteOrder records a frame at RTP position pos. RTP packets arrive in decode
// order and carry the presentation time, so a frame positioned before earlier
// frames was reordered: the encoder uses B-frames. The depth is how many earlier
// frames it overtakes.
func (t *trackFrames) noteOrder(pos int64) {
	overtaken := 0
	for _, r := range t.recent[:min(t.recentN, reorderWindow)] {
		if r > pos {
			overtaken++
		}
	}
	if overtaken > 0 {
		t.reordered++
		t.reorderDepth = max(t.reorderDepth, overtaken)
	}
	t.recent[t.recentN%reorderWindow] = pos
	t.recentN++
}

// flushRun turns the pending run into timestamp jumps.
func (t *trackFrames) flushRun() {
	t.jumps += uint64(t.runN)
	for range t.runKeys {
		t.recordKey(t.pos)
	}
	t.resetRun()
}

func (t *trackFrames) resetRun() {
	t.runN, t.runPos, t.runFirst, t.runKeys = 0, 0, 0, nil
}

// noteKey counts a keyframe, once per RTP timestamp.
func (t *trackFrames) noteKey() {
	if t.keyAtTS {
		return
	}
	t.keyAtTS = true
	t.keyframes++
	if t.runN > 0 {
		t.runKeys = append(t.runKeys, t.runPos)
		return
	}
	t.recordKey(t.pos)
}

func (t *trackFrames) recordKey(pos int64) {
	if !t.haveKey {
		t.haveKey = true
		t.firstKeyPos = pos
	}
	t.lastKeyPos = pos
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
	if t.h265 && t.frames == 0 && t.broken > 0 {
		return nil, "no access unit completed on this H.265 track (marker bit never set?)"
	}
	fs := &FrameStats{
		Frames:         t.frames,
		Keyframes:      t.keyframes,
		TimestampJumps: t.jumps + uint64(t.runN),
		BrokenFrames:   t.broken,
		HasParamSets:   t.haveParams&t.needParams == t.needParams,
		AdvertisedFPS:  round2(t.advertised),

		ReorderedFrames: t.reordered,
		MaxReorderDepth: t.reorderDepth,
	}
	if t.declared >= 0 {
		fs.DeclaredReorderDepth = new(t.declared)
	}
	n := t.nDelta + int64(len(t.seed))
	if n >= 1 && t.pos > 0 {
		fs.FPS = round2(float64(n) * t.clock / float64(t.pos))
	}
	if t.keyframes >= 2 && t.lastKeyPos > t.firstKeyPos {
		fs.KeyframeIntervalMs = round2(float64(t.lastKeyPos-t.firstKeyPos) / float64(t.keyframes-1) / t.clock * 1000)
	}
	return fs, ""
}

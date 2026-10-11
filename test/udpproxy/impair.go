package main

import (
	"math/rand"
	"sync"
	"time"
)

// impairment decides what happens to each RTP packet travelling to the client.
type impairment struct {
	loss      float64       // probability of dropping a packet
	delayMin  time.Duration // each packet is held for a random time in [delayMin, delayMax]
	delayMax  time.Duration
	spikeProb float64       // probability that a packet gets an extra spike delay
	spike     time.Duration // the extra delay; with ordered delivery it stalls the packets behind it too
	reorder   bool          // allow delayed packets to overtake earlier ones
	dropIDR   int           // drop one fragment of the Nth H.264 IDR frame (0: never)
	rng       *rand.Rand
	mu        sync.Mutex
	lastAt    time.Time
	idrCount  int
	inTarget  bool
	idrDone   bool
	Forwarded uint64
	Dropped   uint64
	IDRDrops  uint64
}

func newImpairment(loss float64, delayMin, delayMax time.Duration, spikeProb float64, spike time.Duration, reorder bool, dropIDR int, seed int64) *impairment {
	if delayMax < delayMin {
		delayMax = delayMin
	}
	return &impairment{loss: loss, delayMin: delayMin, delayMax: delayMax, spikeProb: spikeProb, spike: spike, reorder: reorder, dropIDR: dropIDR, rng: rand.New(rand.NewSource(seed))}
}

// decide returns whether to drop the packet and, if not, when to release it.
func (im *impairment) decide(pkt []byte, now time.Time) (drop bool, at time.Time) {
	im.mu.Lock()
	defer im.mu.Unlock()
	if im.idrFragmentDrop(pkt) || (im.loss > 0 && im.rng.Float64() < im.loss) {
		im.Dropped++
		return true, time.Time{}
	}
	im.Forwarded++
	d := im.delayMin
	if im.delayMax > im.delayMin {
		d += time.Duration(im.rng.Int63n(int64(im.delayMax - im.delayMin)))
	}
	if im.spikeProb > 0 && im.rng.Float64() < im.spikeProb {
		d += im.spike
	}
	at = now.Add(d)
	if !im.reorder {
		if at.Before(im.lastAt) {
			at = im.lastAt
		}
		im.lastAt = at
	}
	return false, at
}

// idrFragmentDrop drops the first middle fragment of the Nth H.264 IDR frame
// (FU-A, NAL type 5), which breaks exactly that frame.
func (im *impairment) idrFragmentDrop(pkt []byte) bool {
	if im.dropIDR <= 0 || im.idrDone {
		return false
	}
	if len(pkt) < 12 {
		return false
	}
	off := 12 + 4*int(pkt[0]&0x0f)
	if pkt[0]&0x10 != 0 { // header extension
		if len(pkt) < off+4 {
			return false
		}
		off += 4 + 4*(int(pkt[off+2])<<8|int(pkt[off+3]))
	}
	if len(pkt) < off+2 || pkt[off]&0x1f != 28 { // not FU-A
		return false
	}
	fu := pkt[off+1]
	start, end := fu&0x80 != 0, fu&0x40 != 0
	if start && fu&0x1f == 5 {
		im.idrCount++
		im.inTarget = im.idrCount == im.dropIDR
		return false
	}
	if !im.inTarget {
		return false
	}
	if end {
		im.inTarget = false
		return false
	}
	im.inTarget, im.idrDone = false, true
	im.IDRDrops++
	return true
}

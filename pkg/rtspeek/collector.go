package rtspeek

import (
	"sync"
	"time"

	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/pion/rtp"
)

// collector gathers per-packet measurements that gortsplib's Stats() does not
// provide. Packet callbacks arrive on gortsplib goroutines, so it is locked.
type collector struct {
	mu     sync.Mutex
	tracks map[*description.Media]*trackCollector
	once   sync.Once
	first  chan struct{} // closed on the first packet
	firstT time.Time
}

type trackCollector struct {
	clock        float64 // RTP clock rate in Hz
	packets      uint64
	haveLast     bool
	lastArrival  time.Time
	lastTS       uint32
	jitter       float64 // RFC 3550 interarrival jitter, in RTP clock units
	jitterN      int     // jitter samples seen so far
	jitterSteady float64 // the same estimate, started after the warm-up samples
	jitterPeak   float64 // highest value of jitterSteady
}

// jitterWarmup is how many samples are folded in before the peak is tracked.
// The first packets arrive in a burst while the stream starts up, which would
// otherwise be reported as jitter. The peak is tracked on a separate estimate
// that starts after them, so the tail of that burst does not count either.
const jitterWarmup = 32

func newCollector() *collector {
	return &collector{
		tracks: make(map[*description.Media]*trackCollector),
		first:  make(chan struct{}),
	}
}

// packet records one inbound RTP packet.
func (c *collector) packet(m *description.Media, f format.Format, p *rtp.Packet, now time.Time) {
	c.once.Do(func() {
		c.mu.Lock()
		c.firstT = now
		c.mu.Unlock()
		close(c.first)
	})

	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.tracks[m]
	if t == nil {
		t = &trackCollector{clock: float64(f.ClockRate())}
		c.tracks[m] = t
	}
	t.packets++
	if t.clock > 0 && t.haveLast {
		// B-frames put packets out of timestamp order. Such a packet says nothing
		// about network jitter, so leave it out and keep the last in-order one as
		// the reference. A step back of a second or more is a real discontinuity.
		if back := int64(int32(p.Timestamp - t.lastTS)); /* #nosec G115 -- RFC 3550 signed 32-bit timestamp difference */ back < 0 && float64(-back) < t.clock {
			return
		}
		// D = (arrival delta) - (RTP timestamp delta), both in clock units.
		arrival := now.Sub(t.lastArrival).Seconds() * t.clock
		media := float64(int32(p.Timestamp - t.lastTS)) // #nosec G115 -- RFC 3550 signed 32-bit timestamp difference
		d := arrival - media
		if d < 0 {
			d = -d
		}
		t.jitter += (d - t.jitter) / 16
		t.jitterN++
		if t.jitterN > jitterWarmup {
			t.jitterSteady += (d - t.jitterSteady) / 16
			t.jitterPeak = max(t.jitterPeak, t.jitterSteady)
		}
	}
	t.haveLast = true
	t.lastArrival = now
	t.lastTS = p.Timestamp
}

// firstPacketTime reports when the first packet arrived, if one has.
func (c *collector) firstPacketTime() (time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.firstT, !c.firstT.IsZero()
}

// jitterPeakMs returns the highest jitter seen on track m in milliseconds. The
// RFC 3550 value at the end of the window hides a burst of delay that ended
// earlier, so this is what shows intermittent congestion.
func (c *collector) jitterPeakMs(m *description.Media) float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.tracks[m]
	if t == nil || t.clock <= 0 {
		return 0
	}
	return t.jitterPeak / t.clock * 1000
}

// jitterMs returns the jitter of track m in milliseconds.
func (c *collector) jitterMs(m *description.Media) float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.tracks[m]
	if t == nil || t.clock <= 0 {
		return 0
	}
	return t.jitter / t.clock * 1000
}

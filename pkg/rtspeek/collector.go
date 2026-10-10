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
	clock       float64 // RTP clock rate in Hz
	packets     uint64
	haveLast    bool
	lastArrival time.Time
	lastTS      uint32
	jitter      float64 // RFC 3550 interarrival jitter, in RTP clock units
}

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
		// D = (arrival delta) - (RTP timestamp delta), both in clock units.
		arrival := now.Sub(t.lastArrival).Seconds() * t.clock
		media := float64(int32(p.Timestamp - t.lastTS))
		d := arrival - media
		if d < 0 {
			d = -d
		}
		t.jitter += (d - t.jitter) / 16
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

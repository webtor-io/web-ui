// Package ratemeter turns a monotonically growing byte counter sampled at
// irregular moments into a smoothed bytes-per-second figure.
//
// The seeder reports Completed (verified bytes) about once a second; the
// difference between two samples is the swarm's useful throughput over that
// interval. Raw deltas jump with piece boundaries (a 4 MiB piece completing in
// one tick reads as 4 MB/s, the next tick as 0), so the meter keeps an
// exponential moving average — what every torrent client shows as "download
// speed".
package ratemeter

import "time"

// Meter is not safe for concurrent use; callers sample from one goroutine.
type Meter struct {
	alpha   float64
	lastAt  time.Time
	lastVal int64
	rate    float64 // bytes per second, smoothed
	samples int
	primed  bool
}

// minInterval is the shortest span a delta is measured over. Stats events
// arrive in bursts — the stream's full snapshot and the first diff can be
// milliseconds apart, and a proxy flushes several at once — and 4 MiB over
// 10 ms reads as 400 MB/s. Samples closer than this are folded into the
// next one instead of being measured.
const minInterval = 500 * time.Millisecond

// maxRate is the sanity ceiling, 1 Gbit/s: no swarm feeds a pod faster, so
// an instantaneous figure above it is a counter jump (re-check, seeder
// swap, reconnect onto a pod that knows more), and re-primes the meter
// rather than entering the average.
const maxRate = 125_000_000.0

// New returns a meter with the given smoothing factor in (0, 1]; 0.4 follows
// a change to ~90% within four samples while flattening piece-boundary spikes.
func New(alpha float64) *Meter {
	if alpha <= 0 || alpha > 1 {
		alpha = 0.4
	}
	return &Meter{alpha: alpha}
}

// Sample records the counter's value at t and returns the smoothed rate.
// The first sample only primes the meter (no interval yet → 0). A counter
// that went backwards (torrent re-checked, seeder restarted) re-primes
// instead of reporting a negative rate; an interval shorter than
// minInterval waits for the next sample; a jump above maxRate re-primes.
func (m *Meter) Sample(value int64, t time.Time) float64 {
	if !m.primed || value < m.lastVal {
		m.lastVal, m.lastAt, m.primed = value, t, true
		return m.rate
	}
	if t.Sub(m.lastAt) < minInterval {
		return m.rate
	}
	dt := t.Sub(m.lastAt).Seconds()
	inst := float64(value-m.lastVal) / dt
	if inst > maxRate {
		m.lastVal, m.lastAt = value, t
		return m.rate
	}
	m.lastVal, m.lastAt = value, t
	if m.samples == 0 {
		m.rate = inst
	} else {
		m.rate = m.alpha*inst + (1-m.alpha)*m.rate
	}
	m.samples++
	return m.rate
}

// Rate is the last smoothed value without adding a sample.
func (m *Meter) Rate() float64 { return m.rate }

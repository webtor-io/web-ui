package scripts

import (
	"testing"
	"time"
)

const mb = 1024 * 1024

func TestLowerBoundSpeed(t *testing.T) {
	if got := lowerBoundSpeed(10*mb, 5*time.Second); got != 2*mb {
		t.Fatalf("10MB in 5s: got %v, want %v", got, 2*mb)
	}
	// Nothing to divide, or nothing arrived: unknown, not infinite.
	if got := lowerBoundSpeed(10*mb, 0); got != 0 {
		t.Fatalf("zero elapsed: got %v", got)
	}
	if got := lowerBoundSpeed(0, time.Second); got != 0 {
		t.Fatalf("zero bytes: got %v", got)
	}
}

func TestNeedsFullMeasure(t *testing.T) {
	const bitrate = 8_000_000 // 8 Mbit/s = 1 MB/s
	cases := []struct {
		name       string
		lowerBound float64
		bitrate    int64
		quick      int
		full       int
		want       bool
	}{
		// 10MB in 4 s, peer discovery included: 2.5 MB/s clears 1 MB/s on
		// the pessimistic figure. This is the viewer the 10MB warm-up is for.
		{"fast swarm plays at once", 2.5 * mb, bitrate, 10 * mb, 50 * mb, false},
		// 10MB in 20 s: either a 0.5 MB/s swarm or 15 s of finding peers and
		// a fast one. Indistinguishable here, so it is measured.
		{"ambiguous swarm is measured", 0.5 * mb, bitrate, 10 * mb, 50 * mb, true},
		// Exactly at the bitrate passes, as the gate itself reads it (<).
		{"at the bitrate passes", 1_000_000, bitrate, 10 * mb, 50 * mb, false},
		// A small file has nothing beyond the quick range to measure on.
		{"small file cannot be measured further", 0.1 * mb, bitrate, 6 * mb, 6 * mb, false},
		{"unknown bitrate has no gate", 0.1 * mb, 0, 10 * mb, 50 * mb, false},
	}
	for _, c := range cases {
		if got := needsFullMeasure(c.lowerBound, c.bitrate, c.quick, c.full); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestHalfCapped(t *testing.T) {
	if got := halfCapped(10*mb, 700*mb); got != 10*mb {
		t.Fatalf("large file: got %v", got)
	}
	if got := halfCapped(50*mb, 12*mb); got != 6*mb {
		t.Fatalf("small file: got %v, want half", got)
	}
	// An unknown size (0) must not zero the warm-up.
	if got := halfCapped(10*mb, 0); got != 10*mb {
		t.Fatalf("unknown size: got %v", got)
	}
}

// The measuring constants are what keeps the gate honest against the
// seeder's piece-granular, range-clipped counter; the quick warm-up is what
// the viewer waits for. Shrinking the former to match the latter is the
// mistake this guards (2026-09-20).
func TestWarmupSizes(t *testing.T) {
	if streamWarmupSize >= bandwidthTestSize {
		t.Fatalf("quick warm-up (%d) must be smaller than the measuring range (%d)", streamWarmupSize, bandwidthTestSize)
	}
	if bandwidthTestSize-bandwidthSkipSize < 32*mb {
		t.Fatalf("measuring window %d is too small for 16MB pieces", bandwidthTestSize-bandwidthSkipSize)
	}
}

package statusview

import (
	"testing"
	"time"
)

// The swarm sends pieces in bursts: between two of them nothing moves for a
// few seconds while the transfer is fine. The chain keeps the swarm for
// HoldFor after it last moved, at the rate it last moved at, and takes it
// back the moment it moves again.
func TestHold_KeepsTheSwarmThroughAGap(t *testing.T) {
	t0 := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	r38, r12 := mbpsBytes(38), mbpsBytes(12)
	var h Hold
	if got := h.Swarm(0, t0); got != 0 {
		t.Errorf("never moved: nothing to hold, got %v", got)
	}
	if got := h.Swarm(r38, t0); got != r38 {
		t.Errorf("moving: its own rate, got %v", got)
	}
	if got := h.Swarm(0, t0.Add(HoldFor-time.Millisecond)); got != r38 {
		t.Errorf("inside the hold: the rate it last moved at, got %v", got)
	}
	if got := h.Swarm(0, t0.Add(HoldFor)); got != 0 {
		t.Errorf("the hold is over, got %v", got)
	}
	if got := h.Swarm(r12, t0.Add(HoldFor+time.Second)); got != r12 {
		t.Errorf("moving again: back at once, got %v", got)
	}
	if got := h.Swarm(0, t0.Add(2*HoldFor)); got != r12 {
		t.Errorf("the hold counts from the last movement and keeps its rate, got %v", got)
	}
	if got := h.Swarm(0, t0.Add(2*HoldFor+time.Second)); got != 0 {
		t.Errorf("over again, got %v", got)
	}
	// A rate below any label is not movement.
	if got := h.Swarm(mbpsBytes(0.04), t0.Add(5*HoldFor)); got != 0 {
		t.Errorf("0.04 Mbps: not moving, got %v", got)
	}
	if HoldFor != 10*time.Second {
		t.Errorf("HoldFor %v: HLS and pieces arrive in bursts of seconds", HoldFor)
	}
}

// The viewer's stream to thp is lost (a pod rotation, an ingress reload)
// and reopened with backoff; the reopened stream's first event carries no
// speed. Through that the viewer stays on the chain as last read -- for
// HoldFor, and only while a reopen is on its way.
func TestHold_KeepsTheViewerThroughALostStream(t *testing.T) {
	t0 := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	flow := Viewer{Known: true, Present: true, Mbps: 24, CapMbps: 5}
	var h Hold
	if got := h.Viewer(Viewer{}, true, t0); got.Known {
		t.Errorf("nothing read yet: nothing to hold, got %+v", got)
	}
	if got := h.Viewer(flow, false, t0); got != flow {
		t.Errorf("a reading: as it is, got %+v", got)
	}
	if got := h.Viewer(Viewer{}, true, t0.Add(HoldFor-time.Millisecond)); got != flow {
		t.Errorf("lost, inside the hold: the last reading, got %+v", got)
	}
	if got := h.Viewer(Viewer{}, true, t0.Add(HoldFor)); got.Known {
		t.Errorf("lost past the hold: unknown, got %+v", got)
	}
	// Given up, final, or an open stream gone silent: nothing is on its
	// way, and "we do not know" is not drawn.
	h = Hold{}
	h.Viewer(flow, false, t0)
	if got := h.Viewer(Viewer{}, false, t0.Add(time.Second)); got.Known {
		t.Errorf("not lost: unknown at once, got %+v", got)
	}
	// A known absence is not a gap: numbers arrive, and no request of the
	// viewer's is open.
	h = Hold{}
	h.Viewer(flow, false, t0)
	zero := Viewer{Known: true, CapMbps: 5}
	if got := h.Viewer(zero, true, t0.Add(time.Second)); got != zero {
		t.Errorf("a known zero: as it is, got %+v", got)
	}
	// The hold keeps the last reading that had the viewer on the chain.
	h = Hold{}
	h.Viewer(flow, false, t0)
	h.Viewer(zero, false, t0.Add(time.Second))
	if got := h.Viewer(Viewer{}, true, t0.Add(2*time.Second)); got != flow {
		t.Errorf("after a zero: still the last participation, got %+v", got)
	}
}

// What moves is the swarm sending to Webtor -- caching, or through it into
// Vault -- at a rate its label shows.
func TestSwarmMoving(t *testing.T) {
	for _, c := range []struct {
		tr   Torrent
		want bool
	}{
		{caching(43, 14, 38), true},
		{caching(43, 14, 0.04), false},
		{caching(43, 14, 0), false},
		{Torrent{State: "vaulting", RateBps: mbpsBytes(22)}, true},
		{Torrent{State: "vault_failed", RateBps: mbpsBytes(22)}, false},
		{Torrent{State: "cached", RateBps: mbpsBytes(22)}, false},
		{Torrent{State: "idle", RateBps: mbpsBytes(22)}, false},
	} {
		if got := SwarmMoving(c.tr); got != c.want {
			t.Errorf("%+v: %v, want %v", c.tr, got, c.want)
		}
	}
}

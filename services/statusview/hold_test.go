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
	// That move came 11 s after the one before it: its hold is half as
	// long again as that gap (TestHold_SlowSwarmStaysOnTheChain).
	if got := h.Swarm(0, t0.Add(HoldFor+time.Second+16500*time.Millisecond-time.Millisecond)); got != r12 {
		t.Errorf("an 11 s gap: held 16.5 s, got %v", got)
	}
	if got := h.Swarm(0, t0.Add(HoldFor+time.Second+16500*time.Millisecond)); got != 0 {
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

// A slow swarm with big pieces: 1.2 Mbps verifies a 4 MiB piece every
// ~27 s, and a hold of HoldFor gave the chain to the badge for the rest of
// every gap -- 46, 23 and 11 switches in 300 s for 2, 4 and 8 MiB pieces.
// Once its gap is known, the hold stretches over it: the badge only in the
// first gap, before the second piece says how long one is. The loop calls
// the hold more than once on a piece -- its stats frame, a tick, a thp
// event inside the half second the swarm moves (movingFor) -- and the
// second such call is no gap: measured there, the hold was back to HoldFor
// (33.7, 17.3 and 9.1 switches with random phases).
func TestHold_SlowSwarmStaysOnTheChain(t *testing.T) {
	t0 := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	for _, mib := range []float64{2, 4, 8} {
		period := int(mib * 8 / 1.2)
		var h Hold
		flips, last := 0, ""
		for s := 0; s < 300; s++ {
			calls := []time.Duration{0}
			moving := 0.0
			if s%period == 0 {
				moving = mbpsBytes(1.2)
				calls = append(calls, 300*time.Millisecond)
			}
			for _, d := range calls {
				tr := caching(40, 2, 0)
				tr.RateBps = moving
				v := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: tr, Viewer: zero, ClaimCapMbps: 5, HeldBps: h.Swarm(moving, t0.Add(time.Duration(s)*time.Second+d))})
				if last != "" && v.Mode != last {
					flips++
				}
				last = v.Mode
			}
		}
		if flips > 2 {
			t.Errorf("%v MiB pieces, one every %d s: %d switches between the chain and the badge in 300 s", mib, period, flips)
		}
	}
	// A swarm back after minutes of nothing is not on a cadence of
	// minutes: the stretch stops at maxHold.
	var h Hold
	r := mbpsBytes(1.2)
	h.Swarm(r, t0)
	h.Swarm(0, t0.Add(time.Minute))
	h.Swarm(r, t0.Add(5*time.Minute))
	if got := h.Swarm(0, t0.Add(5*time.Minute+maxHold)); got != 0 {
		t.Errorf("held %v past maxHold after a five-minute gap", got)
	}
}

// A fast swarm moves in every call: its frame each second, and the tick and
// a thp event within the half second after it (movingFor), so no call sees
// it still. The gap it was held for came from its last pause -- the
// player's, a seek, a cold start -- and stayed: after 40 s of nothing and
// then five minutes of a frame every second, its stop kept the chain for a
// minute. Moving for longer than HoldFor, it has no gap any more.
func TestHold_AFastSwarmForgetsAnOldGap(t *testing.T) {
	t0 := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	at := func(ms int) time.Time { return t0.Add(time.Duration(ms) * time.Millisecond) }
	r := mbpsBytes(38)
	var h Hold
	h.Swarm(r, t0)
	for s := 1; s < 40; s++ {
		h.Swarm(0, at(s*1000))
	}
	last := 0
	for s := 40; s < 340; s++ {
		h.Swarm(r, at(s*1000))
		h.Swarm(r, at(s*1000+300))
		last = s*1000 + 300
	}
	ms := last
	for h.Swarm(0, at(ms)) != 0 {
		ms += 100
	}
	if held := time.Duration(ms-last) * time.Millisecond; held > HoldFor {
		t.Errorf("held %v after a stop, a 40 s pause five minutes before it", held)
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

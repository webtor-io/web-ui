package statusview

import "time"

// HoldFor is how long the swarm's last moving reading stays on the chain.
// The seeder's counter can stand still for seconds while the transfer is
// fine, and a chain that fell back to the badge in every such gap would
// blink. Not because it counts whole pieces, as this once said: it takes
// chunks as they arrive (handlers/resource statsWatch.frame); what makes a
// slow swarm's gaps is not established. The viewer is not held by speed at
// all (owner, 2026-09-25): they are on the chain while their requests are
// open, with the Meter's PresenceDebounce, and the page keeps them there
// while its own player plays (View.Playing). Hold.Viewer covers only a gap
// in the data -- a stream to thp lost and being reopened.
const HoldFor = 10 * time.Second

// maxHold: a slow swarm's hold stretches over its gap between two pieces
// (Hold.Swarm), never past this -- a swarm back after minutes of nothing is
// not on a cadence of minutes, and a pause after it must reach the badge.
const maxHold = time.Minute

// Hold is the chain's memory for one status stream. Without a viewer, the
// chain turns into the badge HoldFor after the swarm last moved. With a
// viewer using an incomplete source, the swarm stays after that too, but
// its arrow stops (participants owns the route). Through the hold it is
// drawn as it last moved, with its last speed. Pure:
// every call takes the time it happens at. Not safe for concurrent use; the
// status loop owns it.
type Hold struct {
	at  time.Time
	bps float64
	// gap is the time between the swarm's last two moves, measured across
	// a still call (still): the loop calls more than once on one piece.
	// run is when the moving calls since the last still one began: a fast
	// swarm moves in every call, and a gap from its last pause, held on to
	// past HoldFor of that, kept its stop on the chain for up to maxHold.
	gap   time.Duration
	still bool
	run   time.Time
	// viewer is the last reading that put the viewer on the chain, taken
	// at viewerAt.
	viewer   Viewer
	viewerAt time.Time
}

// Swarm folds the swarm at now: bps is its rate while it moves -- its bytes
// arrived just now, at a rate its label shows (the status loop's word) --
// and 0 while it does not. It returns the rate the chain draws the swarm at
// (Input.HeldBps): bps while it moves, the last one for the hold after it
// stopped, 0 once the hold is over or before it ever moved.
//
// The hold is HoldFor, or half as long again as the gap between the last two
// moves when that is longer, up to maxHold: a slow swarm's jumps (1.2 Mbps
// as 4 MiB every ~27 s) came with the badge for the rest of every gap. The
// first gap is not known until the second jump.
func (h *Hold) Swarm(bps float64, now time.Time) float64 {
	if Quantize(BytesToMbps(bps)) > 0 {
		if h.at.IsZero() || h.still {
			if !h.at.IsZero() {
				h.gap = now.Sub(h.at)
			}
			h.run = now
		} else if now.Sub(h.run) > HoldFor {
			h.gap = 0
		}
		h.at, h.bps, h.still = now, bps, false
		return bps
	}
	h.still = true
	if !h.at.IsZero() && now.Sub(h.at) < min(max(HoldFor, h.gap*3/2), maxHold) {
		return h.bps
	}
	return 0
}

// Moved: the swarm has moved on this stream at least once.
func (h *Hold) Moved() bool { return !h.at.IsZero() }

// Viewer folds the viewer's reading at now. lost: the stream that gave the
// readings is gone and another is on its way (a thp pod rotated, an ingress
// reloaded -- neither final nor out of retries), so an unknown reading is a
// gap in the data, not a viewer who left. Through such a gap the last
// reading that had the viewer on the chain stands in for HoldFor; anything
// else is returned as it is -- a viewer the proxy never said anything about,
// or refused to, is still never drawn.
func (h *Hold) Viewer(v Viewer, lost bool, now time.Time) Viewer {
	if takesPart(v) {
		h.viewer, h.viewerAt = v, now
		return v
	}
	if !v.Known && lost && !h.viewerAt.IsZero() && now.Sub(h.viewerAt) < HoldFor {
		return h.viewer
	}
	return v
}

// takesPart: the reading puts the viewer on the chain -- a request of theirs
// is open (Viewer.Present), whatever its speed.
func takesPart(v Viewer) bool {
	return v.Known && v.Present
}

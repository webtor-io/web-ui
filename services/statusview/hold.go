package statusview

import "time"

// HoldFor is how long a participant stays on the chain after it last took
// part. The seeder verifies whole pieces: between two of them nothing moves
// for seconds while the transfer is fine, and a chain that fell back to the
// badge in every such gap would blink. The viewer is not held by speed at
// all (owner, 2026-09-25): they are on the chain while their requests are
// open, with the Meter's PresenceDebounce, and the page keeps them there
// while its own player plays (View.Playing). Hold.Viewer covers only a gap
// in the data -- a stream to thp lost and being reopened.
const HoldFor = 10 * time.Second

// Hold is the chain's memory for one status stream: the chain takes a
// participant back the moment it moves, and gives it up only HoldFor after
// it last did -- so the chain turns into the badge only after that long
// without movement, and the badge into the chain at once. Through the hold
// a participant is drawn as it last moved: its last speed, never a pause or
// a dash (those are the badge's story, told once the hold is over). Pure:
// every call takes the time it happens at. Not safe for concurrent use; the
// status loop owns it.
type Hold struct {
	at  time.Time
	bps float64
	// viewer is the last reading that put the viewer on the chain, taken
	// at viewerAt.
	viewer   Viewer
	viewerAt time.Time
}

// Swarm folds the swarm at now: bps is its rate while it moves -- its bytes
// arrived just now, at a rate its label shows (the status loop's word) --
// and 0 while it does not. It returns the rate the chain draws the swarm at
// (Input.HeldBps): bps while it moves, the last one for HoldFor after it
// stopped, 0 once the hold is over or before it ever moved.
func (h *Hold) Swarm(bps float64, now time.Time) float64 {
	if Quantize(BytesToMbps(bps)) > 0 {
		h.at, h.bps = now, bps
		return bps
	}
	if !h.at.IsZero() && now.Sub(h.at) < HoldFor {
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

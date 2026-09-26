package statusview

import (
	"math"
	"time"
)

// Sample is one event of torrent-http-proxy's per-session stream
// (GET /session-stats/<infohash>): what the node delivered to this viewer's
// session for this torrent over the last few seconds.
type Sample struct {
	// WindowSec is thp's window_sec: the span an event averages over once
	// the stream's ring holds that much. 0 -- not said, thp's own
	// (thpWindowSec) taken.
	WindowSec   float64
	BytesPerSec float64
	// Conns is the session's content requests for the torrent open at the
	// moment thp sampled them (once a second): a point sample, not a count
	// over the window. A request that opens and closes between two samples
	// is never in it -- every HLS segment a fast link fetches in a fraction
	// of a second.
	Conns int
	// Active is thp's answer to "was a request of the session for the
	// torrent open at any moment since this stream's previous event" -- the
	// ones that opened and closed between two events included. nil: not
	// said, a thp from before the field (presence).
	Active *bool
	// Rate is the rate claim the limiter applied ("5M"), "" for none.
	Rate string
	// Throttled is the share of the window the limiter held the session's
	// traffic, 0..1; nil — no limiter applied (a plan without a rate).
	Throttled *float64
}

// thpWindowSec is thp's window_sec (sessionStatsWindowSec), taken for an
// event that does not say its own.
const thpWindowSec = 5

// fullWindow reports whether thp's ring covers the whole window at the n-th
// event of a stream. thp averages each event over the span its ring for the
// stream covers, one sample a second: nothing at the first event, then 1, 2,
// ... s, window_sec from the (window_sec+1)-th on (statsRing.event). A young
// ring's reading is not the window's: a player fetching a 3.5 Mbps file's
// 4 s segments at a 5 Mbps cap reads the cap over one or two seconds and
// 0.56-0.76 of it over five (TestMeter_YoungRingNeverTurnsTheVerdictOn).
func (s Sample) fullWindow(n int) bool {
	w := s.WindowSec
	if w <= 0 {
		w = thpWindowSec
	}
	return float64(n-1) >= w
}

// requested: thp saw a request of theirs open -- at some moment since its
// last event when it says so (Active), at the moment it sampled (Conns)
// either way: one open at the sample was open since the last event too.
func (s Sample) requested() bool {
	return s.Conns > 0 || (s.Active != nil && *s.Active)
}

// requestedSinceLast is the plan verdict's "a request of theirs is coming"
// (owner, 2026-09-25): thp's own word when it gives one (Active, the whole
// second since its previous event), the point sample (Conns) only from a
// thp too old to say.
func (s Sample) requestedSinceLast() bool {
	if s.Active != nil {
		return *s.Active
	}
	return s.Conns > 0
}

// presence: this sample puts the viewer on the chain (Viewer.Present). A
// thp that answers Active is taken at its word, whatever the window's bytes.
// One from before the field has only the point sample, which misses a
// player whose every request opens and closes between two samples -- the
// page's own HLS player on a fast link read absent through a whole film
// (2026-09-25) -- so with it the window's bytes count too. The price is a
// tail: such a viewer stays on the chain until thp's window (window_sec,
// 5 s) has emptied, and the debounce after that.
func (s Sample) presence() bool {
	return s.requested() || (s.Active == nil && s.BytesPerSec > 0)
}

// quietEvents is how many of thp's events in a row without a request end
// the viewer's presence: PresenceDebounce of their time with none open.
// An event that answers Active covers the whole second since the previous
// one, so PresenceDebounce/sampleEvery of them are that long. A point
// sample does not: the request it last saw open may have stayed open until
// just before the next sample, so it takes one more. Either way a gap in
// the viewer's requests under 2 s never drops them, one of 3 s or more
// always does, and the drop comes 2-3 s after their last request closed
// (TestMeter_DebounceIsTheViewersTwoSeconds). Counted the point sample's
// way on Active, every close reached the badge a second later: thp's event
// right after a request closes still says active (its ends counter moved),
// so the run without one starts an event later than the run of samples
// with conns 0 did -- an abort took 3-4 s to reach the badge.
func (s Sample) quietEvents() int {
	if s.Active != nil {
		return presenceSamples
	}
	return presenceSamples + 1
}

// Viewer is what the chain may say about the viewer's own transfer.
type Viewer struct {
	// Known is false when there is no data: no stream, a thp without the
	// route, a session thp refused, the first event of a stream, numbers
	// that stopped arriving. The viewer's segment and node are then not
	// drawn at all — "we do not know" is not "nothing flows".
	Known bool
	// Present: the viewer takes part -- thp saw a request of theirs for the
	// torrent open since its last event (Sample.presence: "active", or
	// conns > 0), or did less than PresenceDebounce ago. This alone puts
	// them on the chain (owner, 2026-09-25): the speed is only the number on
	// their segment and never decides whether they are there -- except with
	// a thp that does not say (Sample.presence). The numbers below are the
	// meter's only while Present.
	Present bool
	// Mbps is the quantized speed to the viewer; 0 — no number (yet).
	Mbps float64
	// Stalled: a request is open now (conns) and no bytes arrived for
	// stallAfter.
	Stalled bool
	// Limited: the plan's cap binds -- the fact, the pink link: the verdict
	// held planFactRun events in a row (planState.fact).
	Limited bool
	// PlanBox: the plan box is up -- the verdict held PlanBoxAfter, and
	// since then it has not been off for PlanBoxHold (planState.box). It
	// outlives the fact through a dip under the cap, so an ~80 px box does
	// not come and go with the token bucket's sawtooth or a slow second of
	// the swarm; Build still takes it down at once where nothing may be
	// sold (a stall, few seeders, the viewer gone).
	PlanBox bool
	// CapMbps is the cap thp applied, 0 — none.
	CapMbps float64
}

// The plan verdict — "the node is sending to this viewer at their plan's
// cap" — needs two signals, because each alone lies somewhere:
//
//   - throttled: the share of the window the limiter held the session. A slow
//     swarm never makes the limiter wait, so this keeps "the plan" from being
//     said about a swarm. Absent — no limiter — never limited.
//   - use: bytes delivered over the window against the cap. A player that
//     needs less than the cap cannot keep the window full (hls.js filling a
//     3 Mbps video's buffer under a 5 Mbps cap waits in the limiter for most
//     of each fetch, yet averages 3). The limiter's bucket is per session
//     across torrents, so two downloads sharing the cap each read half and
//     neither is called limited: missing a true case is the safe side.
//
// The On/Off pairs are hysteresis, so a token bucket's sawtooth does not blink
// the verdict. What it shows comes in two steps (owner, 2026-09-25; it used
// to be one, fifteen events, which with thp's five-second window ramping up
// under it made a viewer at the cap wait ~20 s for anything):
//
//   - the fact, the pink link "5 Mbps · cap": after planFactRun events in a
//     row (thp sends one a second), off at the verdict's first miss;
//   - the plan box: after PlanBoxAfter of the verdict, and once up it stays
//     until the verdict has been off for PlanBoxHold -- the box is ~80 px
//     tall, and a dip under the cap must not make the page jump. A small
//     file's burst at the cap shows the fact and never the box.
//
// Both come up only on thp's word that the cap binds now: the verdict turns
// on, and the box rises, only on an event that meets the On pair with a
// request of theirs coming (planLimited without prev) over thp's full window
// (Sample.fullWindow). The rest of a run may be the Off pair's -- the
// sawtooth, the window's tail -- but never the step up: the first seconds of
// a new stream (every token rotation, every reopen) read a spike over one or
// two seconds of the window, and the tail after the requests closed is the
// viewer leaving, not a cap to sell against. The box rises, besides, only on
// an event with a request of theirs open at thp's sample (Conns): "active"
// also says yes on the event that sees the last request close, and a box
// raised there went two seconds later with the viewer (PresenceDebounce) --
// a flash. On the recorded capped streams (testdata/session-stats-at-cap.json)
// it costs nothing: every event the box would have risen on had one open.
//
// The thresholds against the recorded events (2026-09-26, 5M cap, prod thp;
// meter_replay_test.go), use = bytes over the window / the cap:
//
//   - three capped streams, events with throttled >= 0.5 (n = 11, 163, 259):
//     use p05 0.955-0.978 by stream, median 0.99-1.00; once on, the verdict
//     held at use 0.742 at the lowest (0.965 and 0.993 on the other two).
//   - a file under the cap (Sintel from Vault, buffer-fill bursts, n = 30
//     with throttled >= 0.5): use median 0.77, p90 0.917, max 1.016.
//
// planUseOn anywhere in 0.85-0.95 gives the capped streams the same fact and
// box, to the event, and the file under the cap the fact on one event and
// never a box; 0.8 gives that file the fact on 4 events, and dropping use
// altogether (throttled alone) on 21 -- throttled is a sum over parallel
// requests and reads >= 0.5 below the cap. 0.92 and up delays one capped
// stream's fact and box by a second. 0.9 is the highest On with no delay on
// any recorded capped stream. planUseOff 0.7 sits under the lowest use a
// capped stream held the verdict at (0.742): at 0.75 it would have blinked.
const (
	planLimitedOn  = 0.5
	planLimitedOff = 0.3
	planUseOn      = 0.9
	planUseOff     = 0.7
	planFactRun    = 3
)

const (
	// PlanBoxAfter: the verdict held this long -- from its first event to
	// the current one -- and the plan box comes up.
	PlanBoxAfter = 8 * time.Second
	// PlanBoxHold: the box goes this long after the last event with the
	// verdict on.
	PlanBoxHold = 10 * time.Second
)

// The box's times in thp's events, sampleEvery apart: planBoxRun events in a
// row span PlanBoxAfter (the first one and PlanBoxAfter of them after it),
// and the planBoxOff-th event without the verdict is PlanBoxHold after the
// last one with it. Counted on the events, not the wall clock between them,
// like the presence debounce: an event late by a few hundred milliseconds
// must not move the box by a second.
const (
	planBoxRun = int(PlanBoxAfter/sampleEvery) + 1
	planBoxOff = int(PlanBoxHold / sampleEvery)
)

// PlanBoxRun is how many of thp's full-window events at the cap the box
// takes; PlanBoxFromOpen how many events of a stream at the cap from its
// open (the zero-length window, thp's ring filling, then PlanBoxRun of
// them). For the tests that feed a stream enough of them to reach it.
const (
	PlanBoxRun      = planBoxRun
	PlanBoxFromOpen = thpWindowSec + planBoxRun
)

// planLimited is the verdict for one event given the previous one. It turns
// on only while requests come -- thp's word on the second since its previous
// event (Sample.requestedSinceLast), not the window's tail after they
// stopped; it holds while the session still waits and still fills most of
// the cap, and drops when the bytes stop.
func planLimited(prev bool, s Sample) bool {
	capBps := RateBytesPerSec(s.Rate)
	if s.Throttled == nil || capBps <= 0 || s.BytesPerSec <= 0 {
		return false
	}
	use := s.BytesPerSec / capBps
	if prev {
		return *s.Throttled >= planLimitedOff && use >= planUseOff
	}
	return s.requestedSinceLast() && *s.Throttled >= planLimitedOn && use >= planUseOn
}

// planState is the verdict with its run -- how many events in a row it held
// -- and the plan box with the events in a row without the verdict since.
type planState struct {
	on  bool
	run int
	box bool
	off int
}

// step folds one event; full: thp's ring covers the whole window at it
// (Sample.fullWindow). Only such an event meeting the On pair -- rises --
// turns the verdict on or raises the box; one that meets only the Off pair
// holds what is on and counts toward the run. A verdict carried over a
// rotation (StreamRotated) holds through the new stream's young ring.
func (p planState) step(s Sample, full bool) planState {
	rises := full && planLimited(false, s)
	if rises || (p.on && planLimited(true, s)) {
		p.on, p.run, p.off = true, p.run+1, 0
		p.box = p.box || (rises && p.run >= planBoxRun && s.Conns > 0)
		return p
	}
	p.on, p.run = false, 0
	if p.box {
		if p.off++; p.off >= planBoxOff {
			p.box, p.off = false, 0
		}
	}
	return p
}

// fact: the cap is said -- the verdict held planFactRun events in a row.
func (p planState) fact() bool { return p.on && p.run >= planFactRun }

const (
	// emaTau smooths the viewer's speed over about three seconds on top of
	// thp's own five-second window, so the label does not twitch between
	// neighbouring tenths every second.
	emaTau = 3 * time.Second
	// freshAfter: bytes that stopped this long ago ended the last transfer:
	// the next one is measured from its own first sample, not blended into
	// a stale average. Only the number: whether the viewer is on the chain
	// is their requests' (PresenceDebounce).
	freshAfter = 10 * time.Second
	// stallAfter: a request is open and nothing arrives for this long —
	// the node waits for a piece from the swarm. Open at thp's sample
	// (conns), not merely since its last event (active): a request the node
	// holds waiting is open at every sample, while quick ones that each
	// come back with nearly nothing -- an HLS player reloading a playlist
	// that is still growing -- are not a wait.
	stallAfter = 5 * time.Second
	// StaleAfter: thp sends an event a second; this long without one and
	// the numbers on screen are no longer true.
	StaleAfter = 3 * time.Second
	// PresenceDebounce: the viewer stays on the chain until thp has seen no
	// request of theirs (Sample.presence) for this long. A download manager
	// swaps one range request for the next, an HLS player pauses between
	// two segments: a moment without one while the viewer is still there.
	// Counted on thp's events, sampleEvery apart -- two in a row without a
	// request when thp answers active, three when it only has the point
	// sample (Sample.quietEvents) -- not on the wall clock between them: an
	// event late by a few hundred milliseconds must not turn a 1.9 s gap
	// into a drop. A gap shorter than this never drops the viewer; one of
	// 3 s or more does, 2-3 s after their last request closed. The page
	// keeps a viewer whose own player plays on the chain through longer
	// gaps (View.Playing).
	//
	// thp's conns alone could not carry it: it is a point sample, the
	// requests open at the moment thp samples, once a second. A request
	// that opens and closes between two samples is never in it, and a
	// quick HLS segment on a fast link is exactly that -- live on
	// 2026-09-25 a paid viewer's page player read conns 0 in every event at
	// 1.1-2.2 MB/s, "Вы" never came, and the page's player had no last
	// reading to keep them by. thp's "active" (a request open at any moment
	// since its previous event) is what presence is read from.
	PresenceDebounce = 2 * time.Second
	// sampleEvery: thp's session stream sends an event a second.
	sampleEvery = time.Second
)

// presenceSamples is PresenceDebounce in thp's events (Sample.quietEvents).
const presenceSamples = int(PresenceDebounce / sampleEvery)

// Meter turns the events of one viewer's session stream into Viewer
// readings. It is pure: every call takes the time it happens at, so the
// status loop drives it with the wall clock and tests with a fake one. It is
// not safe for concurrent use; the status loop owns it.
type Meter struct {
	// events counts the events of the stream now open: the first one covers
	// a zero-length window and carries no speed, the next ones a window
	// still filling (thp statsRing.event, Sample.fullWindow).
	events  int
	sampled bool
	lastAt  time.Time
	conns   int
	capMbps float64
	plan    planState
	// present: the viewer is on the chain (Viewer.Present); idle counts
	// the events in a row without a request of theirs since.
	present bool
	idle    int
	// ema is the smoothed speed of the present viewer's non-zero samples;
	// emaAt the time of the last one, zero before the first.
	ema   float64
	emaAt time.Time
	// shown is the last non-zero label value of this presence; zeroSince
	// the first zero sample of the current run of them, zero while bytes
	// flow.
	shown     float64
	zeroSince time.Time
	// last is the viewer's last reading on the chain with a number to show
	// (Last), kept after they left and across a reopened stream.
	last Viewer
}

// StreamOpened starts over for a new stream: its first event is skipped and
// nothing measured on the previous one carries over — the gap between them
// is unknown — except the last reading on the chain (Last). That one is
// only ever drawn for the page's own player, which vouches for the viewer
// being there: a thp pod rotation or an ingress reload reopens the stream
// (openFresh), and if its first counted sample fell between two HLS
// segments, the page watching a film read "gone" with nothing to keep them
// on the chain by -- the badge until a later sample happened to catch a
// segment request open.
func (m *Meter) StreamOpened() {
	*m = Meter{last: m.last}
}

// StreamRotated is a planned new stream in place of one that was delivering
// (thp ends each at its token's expiry, and the next one is opened just
// before): its first event is skipped like any first, and everything measured
// carries over — the smoothed speed, the label, the plan's run and its box.
// Starting over there cost the viewer's link a second and the plan box
// fifteen, every ten minutes, on every open page. The new stream's ring is
// young, though (Sample.fullWindow): a verdict that is on holds through it,
// and one that is off cannot turn on until the ring is full.
func (m *Meter) StreamRotated() {
	m.events = 0
}

// Observe folds one event received at now.
func (m *Meter) Observe(s Sample, now time.Time) {
	m.events++
	if m.events == 1 {
		return
	}
	m.sampled = true
	m.lastAt = now
	m.conns = s.Conns
	m.capMbps = RateMbps(s.Rate)
	m.plan = m.plan.step(s, s.fullWindow(m.events))
	was := m.present
	switch {
	case s.presence():
		m.present, m.idle = true, 0
	case m.present:
		m.idle++
		m.present = m.idle < s.quietEvents()
	}
	if !m.present {
		// Gone, or never there: the bytes still in thp's window are not
		// theirs to show, and whoever comes next is a new transfer.
		m.ema, m.emaAt, m.shown, m.zeroSince = 0, time.Time{}, 0, time.Time{}
		return
	}
	v := BytesToMbps(s.BytesPerSec)
	if Quantize(v) == 0 {
		if m.zeroSince.IsZero() {
			m.zeroSince = now
		}
	} else {
		// A new presence, or bytes that stopped long ago, start a new
		// transfer: from its own first sample, not from a stale average.
		if !was || m.emaAt.IsZero() || (!m.zeroSince.IsZero() && now.Sub(m.zeroSince) >= freshAfter) {
			m.ema = v
		} else {
			a := 1 - math.Exp(-now.Sub(m.emaAt).Seconds()/emaTau.Seconds())
			m.ema += a * (v - m.ema)
		}
		m.emaAt = now
		m.zeroSince = time.Time{}
		if q := Quantize(m.ema); q > 0 {
			m.shown = q
		}
	}
	// What stays for the page's player once they leave: the number, never
	// a wait -- a player playing on its buffer is not waiting.
	if m.shown > 0 || m.plan.fact() || m.plan.box {
		m.last = Viewer{Known: true, Present: true, Mbps: m.shown, Limited: m.plan.fact(), PlanBox: m.plan.box, CapMbps: m.capMbps}
	}
}

// Stale reports whether the numbers stopped arriving: a stream that is open
// and silent is not evidence of anything.
func (m *Meter) Stale(now time.Time) bool {
	return m.sampled && now.Sub(m.lastAt) > StaleAfter
}

// Reading is what the chain shows at now. A viewer who is not present is a
// known absence: nothing of theirs is drawn, whatever the window says.
func (m *Meter) Reading(now time.Time) Viewer {
	if !m.sampled || m.Stale(now) {
		return Viewer{}
	}
	v := Viewer{Known: true, CapMbps: m.capMbps}
	if !m.present {
		return v
	}
	v.Present, v.Limited, v.PlanBox = true, m.plan.fact(), m.plan.box
	if !m.zeroSince.IsZero() && m.conns > 0 && now.Sub(m.zeroSince) >= stallAfter {
		v.Stalled = true
		return v
	}
	// Through a sample without bytes the last label stands -- the number,
	// not the presence: that is the requests'.
	v.Mbps = m.shown
	return v
}

// Last is the viewer's last reading on the chain that had a number to show
// -- present, the number and the plan's verdict as they were, never a wait
// -- while the reading at now says they left: numbers arrive, and thp saw no
// request of theirs for PresenceDebounce. The server cannot see the page's
// player, which fetches HLS segments until its buffer is full and then
// nothing for as long as it plays on that buffer; the page draws this
// while its own player plays or buffers (Input.LastViewer,
// View.Playing). Zero otherwise: present now, nothing known, or nothing
// ever shown on this status stream (it survives a reopened thp stream,
// StreamOpened).
func (m *Meter) Last(now time.Time) Viewer {
	if r := m.Reading(now); !r.Known || r.Present {
		return Viewer{}
	}
	return m.last
}

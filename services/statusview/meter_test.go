package statusview

import (
	"testing"
	"time"
)

func TestRateMbps(t *testing.T) {
	cases := map[string]float64{
		"5M": 5, "50M": 50, "5m": 5, " 5M ": 5, "5MB": 5, "2.5M": 2.5,
		"512K": 0.5, "1G": 1024, "": 0, "M": 0, "5": 0, "5X": 0, "-5M": 0,
		"five": 0, "5M5": 0, "0M": 0, "5 Mbp": 0,
	}
	for in, want := range cases {
		if got := RateMbps(in); got != want {
			t.Errorf("RateMbps(%q) = %v, want %v", in, got, want)
		}
	}
	// The limiter's unit: a "5M" session is paced at 5·1024² bits a second.
	if got := RateBytesPerSec("5M"); got != 5*1024*1024/8 {
		t.Errorf("RateBytesPerSec(5M) = %v", got)
	}
}

// A viewer held at a "5M" cap must read "5", the number the cap says — the
// same unit on both sides of the comparison.
func TestBytesToMbps_AgreesWithTheCap(t *testing.T) {
	if got := Quantize(BytesToMbps(RateBytesPerSec("5M"))); got != 5 {
		t.Errorf("at the 5M cap: %v Mbps", got)
	}
}

func TestQuantize(t *testing.T) {
	cases := []struct{ in, want float64 }{
		{0, 0}, {-1, 0}, {0.04, 0}, {0.05, 0.1}, {0.44, 0.4}, {1.24, 1.2}, {1.25, 1.3},
		{4.96, 5}, {5.04, 5}, {9.94, 9.9}, {9.96, 10}, {10.4, 10}, {37.6, 38},
	}
	for _, c := range cases {
		if got := Quantize(c.in); got != c.want {
			t.Errorf("Quantize(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestFormatNumber(t *testing.T) {
	cases := []struct {
		lang string
		v    float64
		want string
	}{
		{"en", 1.2, "1.2"}, {"ru", 1.2, "1,2"}, {"de", 0.6, "0,6"}, {"fr", 38, "38"},
		{"ru", 5, "5"}, {"cs", 2.5, "2,5"}, {"xx-garbage", 1.2, "1.2"},
	}
	for _, c := range cases {
		if got := FormatNumber(c.lang, c.v); got != c.want {
			t.Errorf("FormatNumber(%s, %v) = %q, want %q", c.lang, c.v, got, c.want)
		}
	}
}

func thr(v float64) *float64 { return &v }

// active is s as a thp that reports "active" sends it.
func active(s Sample, a bool) Sample {
	s.Active = &a
	return s
}

// capBps is what a "5M" session is paced at.
const capBps = 5 * 1024 * 1024 / 8

// "The node sends to you at your plan's cap" is earned and sticky: on only
// while a request is open, the limiter waited at least half the time AND the
// window was nearly full of bytes; held through a token bucket's dips until
// the waiting or the bytes clearly stop. No limiter or no cap — never.
func TestPlanLimited(t *testing.T) {
	at := func(use, th float64, conns int) Sample {
		return Sample{BytesPerSec: use * capBps, Conns: conns, Rate: "5M", Throttled: thr(th)}
	}
	cases := []struct {
		name string
		prev bool
		s    Sample
		want bool
	}{
		{"waited most of the window, window full", false, at(1, 0.8, 1), true},
		{"exactly at both on thresholds", false, at(planUseOn, planLimitedOn, 1), true},
		{"throttled below on", false, at(1, 0.45, 1), false},
		// hls.js fetching a 3 Mbps video's segments under a 5 Mbps cap.
		{"bursty fetches under the cap", false, at(0.6, 0.9, 1), false},
		{"just under the use threshold", false, at(0.89, 0.9, 1), false},
		{"in the band, already on: stays on", true, at(0.8, 0.4, 1), true},
		{"exactly at both off thresholds, on: stays on", true, at(planUseOff, planLimitedOff, 1), true},
		{"waiting stopped, on: off", true, at(1, 0.29, 1), false},
		{"bytes fell off, on: off", true, at(0.69, 0.9, 1), false},
		{"swarm-bound: limiter present, never waited", false, at(0.3, 0, 1), false},
		{"no limiter at all", false, Sample{BytesPerSec: capBps, Conns: 1, Rate: "5M"}, false},
		{"no limiter at all, was on", true, Sample{BytesPerSec: capBps, Conns: 1, Rate: "5M"}, false},
		{"limiter but no readable cap", false, Sample{BytesPerSec: capBps, Conns: 1, Throttled: thr(0.9)}, false},
		{"no bytes: not downloading", false, at(0, 0.9, 1), false},
		{"no bytes, was on: off", true, at(0, 0.9, 1), false},
		{"no request open: does not turn on", false, at(1, 0.9, 0), false},
		{"no request open, was on: holds", true, at(1, 0.9, 0), true},
		// thp's "active": a request open since its last event, which the
		// point sample missed -- an HLS segment fetched between two. When
		// thp says it, it is the verdict's word (owner, 2026-09-25), not the
		// point sample; conns only from a thp too old to say.
		{"none open now, one since the last event", false, active(at(1, 0.9, 0), true), true},
		{"thp says none since the last event", false, active(at(1, 0.9, 0), false), false},
		{"thp says none since the last event, conns 1: its word", false, active(at(1, 0.9, 1), false), false},
		{"old thp: a request open at the sample", false, at(1, 0.9, 1), true},
	}
	for _, c := range cases {
		if got := planLimited(c.prev, c.s); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// The owner's numbers (2026-09-25): the fact after three of thp's events in
// a row at the cap, the box after eight seconds of it, and the box gone ten
// seconds after the cap was last seen -- counted on thp's events, one a
// second.
func TestPlanTiming_TheOwnersNumbers(t *testing.T) {
	if planFactRun != 3 || PlanBoxAfter != 8*time.Second || PlanBoxHold != 10*time.Second {
		t.Fatalf("fact %d events, box after %v, held %v", planFactRun, PlanBoxAfter, PlanBoxHold)
	}
	// The first event at the cap and eight seconds of them after it; ten
	// without it after the last one with it.
	if planBoxRun != 9 || planBoxOff != 10 {
		t.Fatalf("box at run %d, gone after %d events without the cap", planBoxRun, planBoxOff)
	}
}

// The fact (the pink link) after planFactRun events of the verdict in a
// row, off at the first miss; the plan box after planBoxRun -- PlanBoxAfter
// of a steady cap -- and once up, until planBoxOff events in a row without
// the verdict: a dip shorter than PlanBoxHold keeps it, and a burst at the
// cap shorter than PlanBoxAfter never raises it.
func TestPlanState_FactAndBox(t *testing.T) {
	on := Sample{BytesPerSec: capBps, Conns: 1, Rate: "5M", Throttled: thr(0.9)}
	off := Sample{BytesPerSec: capBps / 2, Conns: 1, Rate: "5M", Throttled: thr(0.9)}
	var p planState
	for i := 1; i <= planBoxRun; i++ {
		p = p.step(on, true)
		if got, want := p.fact(), i >= planFactRun; got != want {
			t.Fatalf("event %d at the cap: fact %v, want %v", i, got, want)
		}
		if got, want := p.box, i >= planBoxRun; got != want {
			t.Fatalf("event %d at the cap: box %v, want %v", i, got, want)
		}
	}
	// A dip: the fact goes at once, the box stays.
	for i := 1; i < planBoxOff; i++ {
		if p = p.step(off, true); p.fact() || !p.box {
			t.Fatalf("dip, event %d: %+v, want the box without the fact", i, p)
		}
	}
	// Back at the cap inside the hold: the box never went, the fact earns
	// its run again.
	if p = p.step(on, true); !p.box || p.fact() || p.off != 0 {
		t.Fatalf("back at the cap: %+v", p)
	}
	// The cap ends: the box goes at the planBoxOff-th event without it.
	for i := 1; i <= planBoxOff; i++ {
		if p = p.step(off, true); p.box != (i < planBoxOff) {
			t.Fatalf("event %d without the cap: box %v", i, p.box)
		}
	}
	if p != (planState{}) {
		t.Fatalf("after the hold: %+v, want nothing left", p)
	}
	// A small file's burst at the cap: the fact, never the box.
	p = planState{}
	for i := 1; i < planBoxRun; i++ {
		p = p.step(on, true)
	}
	if !p.fact() || p.box {
		t.Fatalf("a burst of %d events: %+v, want the fact and no box", planBoxRun-1, p)
	}
	for i := 0; i < 2*planBoxOff; i++ {
		if p = p.step(off, true); p.box || p.fact() {
			t.Fatalf("after the burst: %+v", p)
		}
	}
	// A bursty stream never holds the verdict long enough for the fact.
	p = planState{}
	for i := 0; i < 4*planBoxRun; i++ {
		s := on
		if i%planFactRun == planFactRun-1 {
			s = off
		}
		if p = p.step(s, true); p.fact() || p.box {
			t.Fatalf("bursty stream: %+v at event %d", p, i)
		}
	}
}

// The step up is thp's word that the cap binds now: the verdict turns on,
// and the box rises, only on an event over thp's full window that meets the
// On pair with a request coming. An event of a young ring (full false) or
// one meeting only the Off pair -- the window's tail after the requests
// closed -- holds what is on and counts toward the run, never more.
func TestPlanState_OnlyAFullWindowAtTheCapStepsUp(t *testing.T) {
	on := Sample{BytesPerSec: capBps, Conns: 1, Rate: "5M", Throttled: thr(0.9)}
	// The window's tail after the close: thp says no request since its
	// last event, the bytes and the waiting still in the window.
	tail := active(Sample{BytesPerSec: 0.76 * capBps, Rate: "5M", Throttled: thr(0.72)}, false)

	// A young ring at the cap: nothing, however long.
	var p planState
	for i := 0; i < 2*planBoxRun; i++ {
		if p = p.step(on, false); p != (planState{}) {
			t.Fatalf("young ring, event %d: %+v, want nothing", i+1, p)
		}
	}
	// On already (a rotation): a young ring holds it and counts.
	p = planState{on: true, run: 1}
	for i := 2; i <= planFactRun; i++ {
		p = p.step(on, false)
	}
	if !p.fact() || p.run != planFactRun {
		t.Fatalf("on, through a young ring: %+v, want the fact", p)
	}

	// The run reaches the box on the tail: no box; the viewer left.
	p = planState{}
	for i := 1; i < planBoxRun; i++ {
		p = p.step(on, true)
	}
	if p = p.step(tail, true); !p.on || p.run != planBoxRun || p.box {
		t.Fatalf("the %d-th event, on the tail: %+v, want held and no box", planBoxRun, p)
	}
	// ... or on a young ring: no box either, until a full window at the cap.
	p = planState{}
	for i := 1; i < planBoxRun; i++ {
		p = p.step(on, true)
	}
	if p = p.step(on, false); !p.on || p.box {
		t.Fatalf("the %d-th event, a young ring: %+v, want held and no box", planBoxRun, p)
	}
	if p = p.step(on, true); !p.box {
		t.Fatalf("the next full window at the cap: %+v, want the box", p)
	}
	// A held box is not raised again, it stays: the tail neither takes it
	// down nor counts as off.
	if p = p.step(tail, true); !p.box || p.off != 0 {
		t.Fatalf("the tail under a box: %+v", p)
	}
}

var t0 = time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

func at(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }

// mbpsSample is an event of a thp that reports "active", in a steady state:
// a request that stays open through the second (conns 1) or none at all
// since the last event (0). The event right after a request closed is not
// one of these -- thp says active there (thpFeed); samples of a thp without
// the field build their Sample by hand.
func mbpsSample(v float64, conns int) Sample {
	return active(Sample{BytesPerSec: v * mbit / 8, Conns: conns, Rate: "5M"}, conns > 0)
}

// thpFeed makes the events of a thp that reports "active" for requests that
// each stay open across events (a download), from their count at each
// event, as thp derives it (statsRing.event): open at the sample, or one
// ended since the previous event -- its ends counter moved. So the event
// right after conns drops still says active: that is where thp sees the
// close, and a conns 1→0 event with active false is one thp never sends.
type thpFeed struct{ conns int }

func (f *thpFeed) ev(v float64, conns int) Sample {
	s := active(Sample{BytesPerSec: v * mbit / 8, Conns: conns, Rate: "5M"}, conns > 0 || f.conns > 0)
	f.conns = conns
	return s
}

// thpOver is thp's event at second t for a viewer whose requests are open
// over reqs ([open, close), in seconds): conns the requests open at t, and
// -- when the thp answers it (withActive) -- active: a request open at any
// moment of (t-1, t], since the previous event (open at t, or ended in
// between). No bytes: presence alone.
func thpOver(reqs [][2]float64, t float64, withActive bool) Sample {
	var s Sample
	act := false
	for _, r := range reqs {
		if r[0] <= t && t < r[1] {
			s.Conns++
		}
		if r[0] <= t && r[1] > t-1 {
			act = true
		}
	}
	if withActive {
		s.Active = &act
	}
	return s
}

// capSample is a session held at its 5M cap for share of the window.
func capSample(share float64) Sample {
	s := mbpsSample(5, 1)
	s.Throttled = &share
	return s
}

// The first event of a stream covers a zero-length window: its "0" is not a
// measurement. Only the second one is.
func TestMeter_FirstEventIsUnknown(t *testing.T) {
	var m Meter
	m.Observe(mbpsSample(0, 0), at(0))
	if r := m.Reading(at(0)); r.Known {
		t.Fatalf("first event read as known: %+v", r)
	}
	m.Observe(mbpsSample(3, 1), at(1))
	if r := m.Reading(at(1)); !r.Known || r.Mbps != 3 {
		t.Fatalf("second event: %+v", r)
	}
	// A reopened stream starts over: its first event is skipped again,
	// and the old numbers do not carry over the gap.
	m.StreamOpened()
	m.Observe(mbpsSample(0, 0), at(10))
	if r := m.Reading(at(10)); r.Known || r.Mbps != 0 {
		t.Fatalf("first event after reopen: %+v", r)
	}
}

// openStream feeds m a new stream's events up to the first over thp's full
// window, from second from: the zero-length one, then ev while thp's ring
// fills (Sample.fullWindow). It returns the second of the first full-window
// event.
func openStream(m *Meter, ev Sample, from int) int {
	m.Observe(Sample{Conns: ev.Conns, Active: ev.Active, Rate: ev.Rate}, at(from))
	for s := 1; s < thpWindowSec; s++ {
		m.Observe(ev, at(from+s))
	}
	return from + thpWindowSec
}

// A planned rotation (the token's expiry) skips the new stream's first
// event and keeps everything else: the speed on screen and the plan's run.
// The verdict holds through the new stream's young ring.
func TestMeter_RotationKeepsTheReading(t *testing.T) {
	var m Meter
	s := openStream(&m, capSample(0.9), 0)
	for end := s + planBoxRun; s < end; s++ {
		m.Observe(capSample(0.9), at(s))
	}
	if r := m.Reading(at(s - 1)); !r.Limited || !r.PlanBox || r.Mbps != 5 {
		t.Fatalf("before: %+v", r)
	}
	m.StreamRotated()
	m.Observe(Sample{Conns: 1, Rate: "5M"}, at(s)) // zero-length window
	if r := m.Reading(at(s)); !r.Known || !r.Limited || !r.PlanBox || r.Mbps != 5 {
		t.Fatalf("the new stream's first event: %+v", r)
	}
	for i := 1; i <= thpWindowSec; i++ {
		m.Observe(capSample(0.9), at(s+i))
		if r := m.Reading(at(s + i)); !r.Limited || !r.PlanBox || r.Mbps != 5 {
			t.Fatalf("the new stream's event %d: %+v", i+1, r)
		}
	}
}

// thp averages each event over the span its ring for the stream covers: 1,
// 2, 3, 4 s for a new stream's events after the first, the full 5 s after
// that. The page's HLS player fetching a 3.5 Mbps file's 4 s segments at a
// 5 Mbps cap reads 0.56-0.76 of the cap over full windows -- never the
// verdict -- but 1.00, 1.00, 0.93 over the first spans of a new stream
// (review F1, 2026-09-26: thp's ring modelled, rotation at phase 0). Judged
// like full windows those turned the fact on at the third event, and it went
// again two events later: the pink link, the cap tag and the fact line
// under the bar, after every token rotation, every reopen, every web-ui
// rollout. A young ring never turns the verdict on.
func TestMeter_YoungRingNeverTurnsTheVerdictOn(t *testing.T) {
	ev := func(use, th float64) Sample {
		s := active(Sample{BytesPerSec: use * capBps, Rate: "5M"}, true)
		s.Throttled = &th
		return s
	}
	young := []Sample{ev(1.00, 1.00), ev(1.00, 1.00), ev(0.93, 0.93), ev(0.70, 0.70)}
	full := []Sample{ev(0.76, 0.76), ev(0.56, 0.56), ev(0.66, 0.66), ev(0.72, 0.72), ev(0.60, 0.60)}
	check := func(name string, m *Meter, s int) {
		t.Helper()
		m.Observe(Sample{Conns: 1, Rate: "5M"}, at(s)) // zero-length window
		for i, e := range append(append([]Sample{}, young...), full...) {
			m.Observe(e, at(s+1+i))
			if r := m.Reading(at(s + 1 + i)); r.Limited || r.PlanBox || m.plan.on {
				t.Fatalf("%s, event %d: %+v, plan %+v -- a young ring's spike read as the cap", name, i+2, r, m.plan)
			}
		}
	}
	// A fresh open (a reopen, a status-stream reconnect).
	var m Meter
	check("fresh", &m, 0)
	// A token rotation after a minute of the same player, never at the cap.
	var r Meter
	s := openStream(&r, ev(0.6, 0.6), 0)
	for end := s + 60; s < end; s++ {
		r.Observe(ev(0.6, 0.6), at(s))
	}
	r.StreamRotated()
	check("rotated", &r, s)

	// The ring is thp's window_sec long, whatever it says: at window_sec 3
	// the 4th event is the first full one.
	var w Meter
	w.Observe(Sample{WindowSec: 3, Conns: 1, Rate: "5M"}, at(0))
	for i := 1; i <= 3; i++ {
		c := capSample(0.9)
		c.WindowSec = 3
		w.Observe(c, at(i))
		if got, want := w.plan.on, i == 3; got != want {
			t.Fatalf("window_sec 3, event %d: on %v, want %v", i+1, got, want)
		}
	}
}

// The tail of a download that closed with the run one short of the box:
// thp's event right after the close still says active (its ends counter
// moved), the one after says no request, with the window still holding the
// bytes and the waiting (review F2, 2026-09-26: use 0.76, throttled 0.72).
// That holds the verdict; it does not raise the ~80 px box for the second
// before the viewer leaves.
func TestMeter_TheTailRaisesNoBox(t *testing.T) {
	var f thpFeed
	var m Meter
	under := func(conns int) Sample {
		s := f.ev(2.5, conns)
		s.Throttled = thr(0.1)
		return s
	}
	atCap := func(use, th float64, conns int) Sample {
		s := f.ev(use*5, conns)
		s.Throttled = thr(th)
		return s
	}
	s := openStream(&m, under(1), 0)
	for i := 1; i < planBoxRun-1; i++ {
		m.Observe(atCap(1, 0.9, 1), at(s))
		s++
	}
	tail := []Sample{
		atCap(0.96, 0.91, 0), // the close: active, the (planBoxRun-1)-th of the run
		atCap(0.76, 0.72, 0), // no request since: the planBoxRun-th, held only
		atCap(0.56, 0.53, 0),
		atCap(0.36, 0.34, 0),
		atCap(0, 0, 0),
	}
	for i, e := range tail {
		m.Observe(e, at(s))
		if r := m.Reading(at(s)); r.PlanBox || m.plan.box {
			t.Fatalf("tail event %d (active %v): %+v, plan %+v -- the box rose after the requests closed", i+1, *e.Active, r, m.plan)
		}
		s++
	}
	if m.Reading(at(s - 1)).Present {
		t.Fatal("still present after the tail: the test proves nothing about the viewer leaving")
	}
}

// Nothing ever flowed and nothing is open: known, and nobody on the chain.
func TestMeter_NotStartedIsZero(t *testing.T) {
	var m Meter
	m.Observe(mbpsSample(0, 0), at(0))
	m.Observe(mbpsSample(0, 0), at(1))
	r := m.Reading(at(1))
	if !r.Known || r.Present || r.Mbps != 0 || r.Stalled {
		t.Fatalf("got %+v", r)
	}
}

// Over about three seconds, not per event: a jump from 10 to 20 shows 13
// first, then climbs.
func TestMeter_EMA(t *testing.T) {
	var m Meter
	m.Observe(mbpsSample(0, 0), at(0))
	m.Observe(mbpsSample(10, 1), at(1))
	m.Observe(mbpsSample(20, 1), at(2))
	if r := m.Reading(at(2)); r.Mbps != 13 {
		t.Fatalf("one second after the jump: %v, want 13 (10 + (1-e^-1/3)·10)", r.Mbps)
	}
	for s := 3; s <= 20; s++ {
		m.Observe(mbpsSample(20, 1), at(s))
	}
	if r := m.Reading(at(20)); r.Mbps != 20 {
		t.Fatalf("settled: %v", r.Mbps)
	}
}

// Samples that print the same label read the same: the stream dedupes on
// the JSON, so a speed wobbling inside one label sends nothing.
func TestMeter_QuantizedToTheLabel(t *testing.T) {
	var m Meter
	m.Observe(mbpsSample(0, 0), at(0))
	seen := map[float64]bool{}
	for s, v := range []float64{5.02, 4.98, 5.01, 4.99, 5.03, 4.97} {
		m.Observe(mbpsSample(v, 1), at(s+1))
		seen[m.Reading(at(s+1)).Mbps] = true
	}
	if len(seen) != 1 || !seen[5] {
		t.Fatalf("labels: %v", seen)
	}
}

// The viewer is on the chain while thp sees a request of theirs, and until
// it has seen none for PresenceDebounce -- counted on thp's own events, one
// a second: with "active", each covering the second before it, the second
// event in a row without a request ends it -- 2-3 s after the close, which
// thp's first event with conns 0 still reports as active. The speed never
// decides: the five-second window still carries the bytes after the
// requests closed (measured 2026-09-25: conns 1→0 within 1.2 s of a client
// abort, the bytes for five more seconds), and a request open with no bytes
// yet is present.
func TestMeter_PresenceIsTheConnections(t *testing.T) {
	var f thpFeed
	var m Meter
	m.Observe(f.ev(0, 0), at(0))
	m.Observe(f.ev(12, 1), at(1))
	if r := m.Reading(at(1)); !r.Known || !r.Present || r.Mbps != 12 {
		t.Fatalf("downloading: %+v", r)
	}
	// Aborted between 1 and 2: thp counts none open at 2, and says one
	// ended since 1; the window's tail still decaying.
	m.Observe(f.ev(10, 0), at(2))
	m.Observe(f.ev(8, 0), at(3))
	if r := m.Reading(at(3)); !r.Present || r.Mbps == 0 {
		t.Fatalf("the close, then one event without a request: inside the debounce: %+v", r)
	}
	// Between thp's events the wall clock decides nothing.
	if r := m.Reading(at(4).Add(500 * time.Millisecond)); !r.Present {
		t.Fatalf("no second event without a request yet: %+v", r)
	}
	m.Observe(f.ev(6, 0), at(4))
	if r := m.Reading(at(4)); !r.Known || r.Present || r.Mbps != 0 || r.Limited || r.Stalled {
		t.Fatalf("the second event without a request -- none open since 2, 2-3 s after the close: gone, bytes in the window or not: %+v", r)
	}
	if PresenceDebounce != 2*time.Second {
		t.Errorf("PresenceDebounce %v", PresenceDebounce)
	}

	// A thp without "active" has only the point sample: the request it
	// last saw open may have stayed open until just before the next one, so
	// the same 2-3 s after the close takes a third sample without one.
	var o Meter
	o.Observe(Sample{Conns: 1}, at(0))
	o.Observe(Sample{Conns: 1}, at(1))
	o.Observe(Sample{}, at(2))
	o.Observe(Sample{}, at(3))
	if r := o.Reading(at(3)); !r.Present {
		t.Fatalf("no active, two samples without a request: inside the debounce: %+v", r)
	}
	o.Observe(Sample{}, at(4))
	if r := o.Reading(at(4)); !r.Known || r.Present {
		t.Fatalf("no active, the third sample without a request: still present %+v", r)
	}

	// A request open and no bytes yet: present, the number not known.
	var c Meter
	c.Observe(mbpsSample(0, 1), at(0))
	c.Observe(mbpsSample(0, 1), at(1))
	if r := c.Reading(at(1)); !r.Present || r.Mbps != 0 || r.Stalled {
		t.Fatalf("connected, no bytes yet: %+v", r)
	}

	// Never connected: absent at once -- the debounce is for a drop only.
	var n Meter
	n.Observe(mbpsSample(0, 0), at(0))
	n.Observe(mbpsSample(3, 0), at(1))
	if r := n.Reading(at(1)); !r.Known || r.Present || r.Mbps != 0 {
		t.Fatalf("never connected: %+v", r)
	}
}

// An external client swaps one range request for the next, and an HLS
// player's requests close between its segments: conns reading zero for a
// sample or two never drops the viewer; a gap through a third sample does --
// thp's first event with conns 0 is the close, still active.
func TestMeter_GapsUnderTheDebounceDoNotDrop(t *testing.T) {
	var f thpFeed
	var m Meter
	m.Observe(f.ev(0, 0), at(0))
	conns := []int{1, 0, 1, 0, 0, 1, 1, 0, 0, 1}
	for i, c := range conns {
		m.Observe(f.ev(4, c), at(i+1))
		if r := m.Reading(at(i + 1)); !r.Present {
			t.Fatalf("sample %d (conns %v): dropped in a gap under the debounce: %+v", i+1, conns[:i+1], r)
		}
	}
	for i := 1; i <= 3; i++ {
		m.Observe(f.ev(4, 0), at(len(conns)+i))
	}
	if r := m.Reading(at(len(conns) + 3)); r.Present {
		t.Fatalf("three samples without a request: still present %+v", r)
	}
}

// PresenceDebounce is the viewer's time, not a count of thp's events: a gap
// in their requests shorter than 2 s never drops them, one of 3 s or more
// always does, and the drop comes 2-3 s after their last request closed --
// whether thp answers "active" (each event covers the second before it: two
// in a row without a request are two seconds with none open) or only counts
// the requests open at its sample (the one it last saw open may have lasted
// until just before the next: three in a row). Every phase of the gap
// against thp's clock; between 2 and 3 s it depends on the phase.
func TestMeter_DebounceIsTheViewersTwoSeconds(t *testing.T) {
	for _, withActive := range []bool{true, false} {
		for _, gap := range []float64{0.3, 1, 1.5, 1.95, 3, 3.5, 6} {
			for p := 0.05; p < 1; p += 0.1 {
				closed := 5 + p
				back := closed + gap
				reqs := [][2]float64{{0.5, closed}, {back, 40}}
				var m Meter
				gone := -1
				for s := 0; s <= 30; s++ {
					m.Observe(thpOver(reqs, float64(s), withActive), at(s))
					if s == 0 {
						continue // the zero-length window
					}
					present := m.Reading(at(s)).Present
					if !present && gone < 0 {
						gone = s
					}
					if float64(s) >= back && !present {
						t.Errorf("active %v, gap %.2f s, closed at %.2f: not back at %d, the request open since %.2f", withActive, gap, closed, s, back)
					}
				}
				switch {
				case gap < 2 && gone >= 0:
					t.Errorf("active %v, gap %.2f s, closed at %.2f: dropped at %d, a gap under the debounce", withActive, gap, closed, gone)
				case gap >= 3 && gone < 0:
					t.Errorf("active %v, gap %.2f s, closed at %.2f: never dropped", withActive, gap, closed)
				case gap >= 3:
					if d := float64(gone) - closed; d < 2 || d >= 3 {
						t.Errorf("active %v, gap %.2f s: dropped %.2f s after the close, want 2-3 s", withActive, gap, d)
					}
				}
			}
		}
	}
}

// hlsSample is thp's event for a page player fetching HLS segments from
// the transcoder on a paid plan with no limiter, as it read live on
// 2026-09-25: 1.1-2.2 MB/s in the window and conns 0 in every event -- each
// segment took well under a second, and the point sample never landed
// inside one. active is thp's "active": a request open since its last event.
func hlsSample(bytesPerSec float64, act bool) Sample {
	return active(Sample{BytesPerSec: bytesPerSec}, act)
}

// The bug the owner saw: streaming in the page's player, "Вы" never came.
// Present is "a request was open since thp's last event" when thp says so
// -- conns, a point sample, missed every one of these segments -- with the
// same debounce on it, and the readings on the chain are kept, so the page's
// player has a last one to stand on once the fetches pause.
func TestMeter_PresenceIsARequestSinceTheLastEvent(t *testing.T) {
	var m Meter
	m.Observe(hlsSample(0, true), at(0)) // the zero-length window
	for i, bps := range []float64{1.1e6, 2.2e6, 1.6e6, 1.1e6, 1.9e6} {
		m.Observe(hlsSample(bps, true), at(i+1))
		if r := m.Reading(at(i + 1)); !r.Known || !r.Present || r.Mbps == 0 || r.Stalled {
			t.Fatalf("event %d, %.1f MB/s, a segment since the last event, conns 0: %+v", i+1, bps/1e6, r)
		}
	}
	// The buffer is full, the player stops fetching: thp says no request
	// since its last event, the window still carries the bytes. The
	// debounce, then gone -- the bytes do not keep them when thp said.
	m.Observe(hlsSample(1.2e6, false), at(6))
	if r := m.Reading(at(6)); !r.Present {
		t.Fatalf("one event without a request, inside the debounce: %+v", r)
	}
	m.Observe(hlsSample(0.8e6, false), at(7))
	if r := m.Reading(at(7)); !r.Known || r.Present {
		t.Fatalf("the second event without a request -- two seconds with none open: still present %+v", r)
	}
	l := m.Last(at(7))
	if !l.Known || !l.Present || l.Mbps == 0 {
		t.Fatalf("no last reading for the page's player: %+v", l)
	}
	// Known false is an answer: never on the chain by the window alone.
	var n Meter
	n.Observe(hlsSample(0, false), at(0))
	n.Observe(hlsSample(1.5e6, false), at(1))
	if r := n.Reading(at(1)); !r.Known || r.Present {
		t.Fatalf("thp said no request since its last event, 1.5 MB/s in the window: %+v", r)
	}
	// A request open at the sample was open since the last event, whatever
	// the flag says.
	var c Meter
	c.Observe(active(mbpsSample(0, 1), false), at(0))
	c.Observe(active(mbpsSample(4, 1), false), at(1))
	if r := c.Reading(at(1)); !r.Present || r.Mbps != 4 {
		t.Fatalf("conns 1, active false: %+v", r)
	}
}

// A request open at some moment of every second, and nothing arrives: not a
// stall. A stall is a request held open with nothing coming, which the point
// sample always sees; quick requests that each come back empty -- an HLS
// player reloading its playlist -- are not a node waiting for a piece.
func TestMeter_StallIsARequestOpenNow(t *testing.T) {
	var m Meter
	m.Observe(hlsSample(0, true), at(0))
	m.Observe(hlsSample(1.1e6, true), at(1))
	for s := 2; s <= 12; s++ {
		m.Observe(hlsSample(0, true), at(s))
	}
	if r := m.Reading(at(12)); !r.Present || r.Stalled {
		t.Fatalf("active, conns 0, no bytes: %+v, want present and not stalled", r)
	}
	var st Meter
	st.Observe(active(mbpsSample(0, 1), true), at(0))
	st.Observe(active(mbpsSample(3, 1), true), at(1))
	for s := 2; s <= 7; s++ {
		st.Observe(active(mbpsSample(0, 1), true), at(s))
	}
	if r := st.Reading(at(7)); !r.Stalled {
		t.Fatalf("a request open now and nothing for 5 s: %+v, want stalled", r)
	}
}

// Not fixed here (review, 2026-09-25): a paused page player whose film the
// transcoder is still making keeps reloading its playlist. The transcoder
// leaves #EXT-X-ENDLIST off until its run completes (and its pacing stops
// FFmpeg well ahead of a paused viewer, so the run does not complete), hls.js
// 1.6.14 reads such a playlist as live and reloads it every target duration,
// half that while it comes back unchanged -- 4 s segments: every 2 s, paused
// or not. thp counts those responses like any request and its event does
// not tell a playlist from a segment, so the viewer reads present: the chain
// for as long as the player stays paused, not the badge. Pinned so the
// docs' "paused with the buffer full" is not taken as the badge: that needs
// thp to leave playlist responses out of its counts.
func TestMeter_PlaylistReloadsKeepAPausedViewer(t *testing.T) {
	const playlist = 40e3 // bytes, one reload of a film's playlist
	reloads := func(every float64, t float64, withActive bool) Sample {
		var s Sample
		act := false
		for r := 0.3; r <= t; r += every {
			if r > t-1 {
				act = true
			}
			if r > t-5 { // thp's window
				s.BytesPerSec += playlist / 5
			}
		}
		if withActive {
			s.Active = &act
		}
		return s
	}
	for _, withActive := range []bool{true, false} {
		var m Meter
		for s := 0; s <= 30; s++ {
			m.Observe(reloads(2, float64(s), withActive), at(s))
			if r := m.Reading(at(s)); s >= 1 && !r.Present {
				t.Errorf("active %v: a reload every 2 s read absent at %d -- the known false chain is gone: thp leaves playlists out now? update the docs", withActive, s)
			}
		}
	}
	// Further apart than the debounce, the chain blinks.
	var m Meter
	on, off := 0, 0
	for s := 0; s <= 30; s++ {
		m.Observe(reloads(4, float64(s), true), at(s))
		if s < 1 {
			continue
		}
		if m.Reading(at(s)).Present {
			on++
		} else {
			off++
		}
	}
	if on == 0 || off == 0 {
		t.Errorf("a reload every 4 s: present %d, absent %d events, want both", on, off)
	}
}

// A thp from before "active" (the field absent): the point sample alone
// would miss the HLS player above, so the window's bytes count too -- at the
// price of a tail: the viewer stays on the chain until the window has
// emptied (window_sec, 5 s) and the debounce after it.
func TestMeter_OldThpFallsBackToTheBytes(t *testing.T) {
	var m Meter
	m.Observe(Sample{}, at(0))
	for i, bps := range []float64{1.1e6, 2.2e6, 1.6e6} {
		m.Observe(Sample{BytesPerSec: bps}, at(i+1))
		if r := m.Reading(at(i + 1)); !r.Known || !r.Present || r.Mbps == 0 {
			t.Fatalf("no active field, conns 0, %.1f MB/s: %+v", bps/1e6, r)
		}
	}
	// Stopped: the window's tail keeps them, then the debounce.
	tail := []float64{1.2e6, 0.8e6, 0.4e6, 0.1e6, 0}
	for i, bps := range tail {
		m.Observe(Sample{BytesPerSec: bps}, at(4+i))
	}
	if r := m.Reading(at(8)); !r.Present {
		t.Fatalf("the first empty window, inside the debounce: %+v", r)
	}
	m.Observe(Sample{}, at(9))
	m.Observe(Sample{}, at(10))
	if r := m.Reading(at(10)); !r.Known || r.Present {
		t.Fatalf("the third empty window: still present %+v", r)
	}
	if l := m.Last(at(10)); !l.Present || l.Mbps == 0 {
		t.Fatalf("no last reading: %+v", l)
	}
	// Nothing in the window and nothing open: never there.
	var n Meter
	n.Observe(Sample{}, at(0))
	n.Observe(Sample{}, at(1))
	if r := n.Reading(at(1)); !r.Known || r.Present {
		t.Fatalf("nothing: %+v", r)
	}
}

// While present, the number is the speed's label: the last one through a
// sample without bytes (until the stall), a new transfer measured from its
// own first sample. When the viewer leaves, their last reading on the chain
// stays for the page's player (Last) -- only while they read absent.
func TestMeter_LastReading(t *testing.T) {
	var m Meter
	m.Observe(mbpsSample(0, 0), at(0))
	m.Observe(mbpsSample(5, 1), at(1))
	m.Observe(mbpsSample(0, 1), at(2))
	if r := m.Reading(at(2)); !r.Present || r.Mbps != 5 {
		t.Fatalf("a sample without bytes, a request open: %+v, want the last 5", r)
	}
	if l := m.Last(at(2)); l != (Viewer{}) {
		t.Fatalf("present: no last reading to stand in, got %+v", l)
	}
	for s := 3; s <= 5; s++ {
		m.Observe(mbpsSample(0, 0), at(s))
	}
	if r := m.Reading(at(5)); r.Present {
		t.Fatalf("left: %+v", r)
	}
	if l := m.Last(at(5)); l != (Viewer{Known: true, Present: true, Mbps: 5, CapMbps: 5}) {
		t.Fatalf("the last reading on the chain: %+v", l)
	}
	if l := m.Last(at(5).Add(StaleAfter + time.Millisecond)); l != (Viewer{}) {
		t.Fatalf("stale: nothing to say, got %+v", l)
	}
	// Back: a new transfer, measured from its own first sample.
	m.Observe(mbpsSample(2, 1), at(6))
	if r := m.Reading(at(6)); !r.Present || r.Mbps != 2 {
		t.Fatalf("back: %+v, want 2", r)
	}
	// Back with no bytes yet: no number from before the gap.
	m.Observe(mbpsSample(0, 0), at(7))
	m.Observe(mbpsSample(0, 0), at(8))
	m.Observe(mbpsSample(0, 0), at(9))
	m.Observe(mbpsSample(0, 1), at(10))
	if r := m.Reading(at(10)); !r.Present || r.Mbps != 0 {
		t.Fatalf("back, no bytes yet: %+v", r)
	}
	// A stall is not what a player playing on its buffer is doing: the last
	// reading keeps the number, not the wait.
	var st Meter
	st.Observe(mbpsSample(0, 0), at(0))
	st.Observe(mbpsSample(6, 1), at(1))
	for s := 2; s <= 7; s++ {
		st.Observe(mbpsSample(0, 1), at(s))
	}
	if r := st.Reading(at(7)); !r.Stalled {
		t.Fatalf("stalled: %+v", r)
	}
	for s := 8; s <= 10; s++ {
		st.Observe(mbpsSample(0, 0), at(s))
	}
	if l := st.Last(at(10)); l != (Viewer{Known: true, Present: true, Mbps: 6, CapMbps: 5}) {
		t.Fatalf("after a stall: %+v", l)
	}
	// A reopened stream (a thp pod rotated) starts over -- no presence, no
	// speed from before the gap -- but the last reading on the chain
	// stays: the page's player, playing across the reopen and between two
	// segments, keeps the viewer on the chain by it.
	st.StreamOpened()
	st.Observe(mbpsSample(0, 0), at(11))
	if l := st.Last(at(11)); l != (Viewer{}) {
		t.Fatalf("reopened, nothing read yet: %+v", l)
	}
	st.Observe(mbpsSample(0, 0), at(12))
	if r := st.Reading(at(12)); !r.Known || r.Present || r.Mbps != 0 {
		t.Fatalf("reopened, no request open: %+v", r)
	}
	if l := st.Last(at(12)); l != (Viewer{Known: true, Present: true, Mbps: 6, CapMbps: 5}) {
		t.Fatalf("reopened: the last reading on the chain is gone: %+v", l)
	}
	// Back on the reopened stream: measured from its own first sample.
	st.Observe(mbpsSample(2, 1), at(13))
	if r := st.Reading(at(13)); !r.Present || r.Mbps != 2 {
		t.Fatalf("back after the reopen: %+v, want 2", r)
	}
}

// A planned rotation keeps the viewer where they are: present stays present.
func TestMeter_RotationKeepsPresence(t *testing.T) {
	var m Meter
	m.Observe(mbpsSample(0, 0), at(0))
	m.Observe(mbpsSample(5, 1), at(1))
	m.StreamRotated()
	m.Observe(Sample{Rate: "5M"}, at(2)) // the new stream's first: skipped
	if r := m.Reading(at(2)); !r.Present || r.Mbps != 5 {
		t.Fatalf("rotated: %+v", r)
	}
}

// Requests open and nothing arrives: after stallAfter that is a stall, even
// inside the hold.
func TestMeter_Stall(t *testing.T) {
	var m Meter
	m.Observe(mbpsSample(0, 0), at(0))
	m.Observe(mbpsSample(5, 1), at(1))
	for s := 2; s <= 6; s++ {
		m.Observe(mbpsSample(0, 1), at(s))
	}
	if r := m.Reading(at(6)); r.Stalled || r.Mbps != 5 {
		t.Fatalf("4 s without bytes: %+v, want the held speed", r)
	}
	m.Observe(mbpsSample(0, 1), at(7))
	if r := m.Reading(at(7)); !r.Stalled || r.Mbps != 0 {
		t.Fatalf("5 s without bytes, a request open: %+v, want stalled", r)
	}
	// No request open: not a stall, just nothing flowing.
	var idle Meter
	idle.Observe(mbpsSample(0, 0), at(0))
	for s := 1; s <= 20; s++ {
		idle.Observe(mbpsSample(0, 0), at(s))
	}
	if r := idle.Reading(at(20)); r.Stalled {
		t.Fatalf("no conns: %+v", r)
	}
}

// thp sends an event a second; numbers that stopped arriving are not true.
func TestMeter_Stale(t *testing.T) {
	var m Meter
	m.Observe(mbpsSample(0, 0), at(0))
	m.Observe(mbpsSample(5, 1), at(1))
	if r := m.Reading(at(1).Add(StaleAfter)); !r.Known {
		t.Fatal("stale too early")
	}
	if r := m.Reading(at(1).Add(StaleAfter + time.Millisecond)); r.Known {
		t.Fatalf("stale reading still known: %+v", r)
	}
}

// The verdict reaches the reading in two steps: the fact (Limited, the pink
// link) at the third event at the cap, the plan box (PlanBox) eight seconds
// into it. The box outlives a dip with the viewer still downloading, and
// goes ten seconds after the cap was last seen; the last reading on the
// chain carries both, for the page's own player.
func TestMeter_FactThenBox(t *testing.T) {
	var m Meter
	below := mbpsSample(2.5, 1)
	below.Throttled = thr(0.1)
	// At the cap from the stream's open: nothing while thp's ring fills.
	s := openStream(&m, capSample(0.9), 0)
	if r := m.Reading(at(s - 1)); r.Limited || r.PlanBox || r.Mbps != 5 {
		t.Fatalf("a young ring at the cap: %+v, want the speed alone", r)
	}
	step := func(ev Sample) Viewer {
		m.Observe(ev, at(s))
		r := m.Reading(at(s))
		s++
		return r
	}
	first := s // the first event over thp's full window
	for s < first+planBoxRun {
		r := step(capSample(0.9))
		n := s - first // events at the cap so far
		if got, want := r.Limited, n >= planFactRun; got != want {
			t.Fatalf("%d s into the cap: fact %v, want %v", n-1, got, want)
		}
		if got, want := r.PlanBox, n >= planBoxRun; got != want {
			t.Fatalf("%d s into the cap: box %v, want %v", n-1, got, want)
		}
	}
	if r := m.Reading(at(s - 1)); !r.Limited || !r.PlanBox || r.CapMbps != 5 || r.Mbps != 5 {
		t.Fatalf("%v into the cap: %+v, want the fact and the box", PlanBoxAfter, r)
	}
	// Five seconds under the cap, still downloading: the pink link goes,
	// the box stays.
	for i := 0; i < 5; i++ {
		if r := step(below); r.Limited || !r.PlanBox || !r.Present || r.Mbps == 0 {
			t.Fatalf("dip, event %d: %+v", i, r)
		}
	}
	// Back at the cap, then off it for good.
	for i := 0; i < planFactRun; i++ {
		step(capSample(0.9))
	}
	last := s - 1
	if r := m.Reading(at(last)); !r.Limited || !r.PlanBox {
		t.Fatalf("back at the cap: %+v", r)
	}
	for {
		r := step(below)
		if !r.Present {
			t.Fatalf("left the chain: %+v", r)
		}
		if !r.PlanBox {
			if gone := time.Duration(s-1-last) * time.Second; gone != PlanBoxHold {
				t.Fatalf("the box went %v after the cap was last seen, want %v", gone, PlanBoxHold)
			}
			break
		}
		if r.Limited {
			t.Fatalf("under the cap, the fact: %+v", r)
		}
	}
	// The last reading on the chain keeps the box as it was -- and the
	// fact as it was then: gone with the bytes, a debounce before the
	// viewer left.
	var l Meter
	b := openStream(&l, capSample(0.9), 0) + planBoxRun // after the run
	for i := b - planBoxRun; i < b; i++ {
		l.Observe(capSample(0.9), at(i))
	}
	for i := b; i < b+3; i++ {
		l.Observe(mbpsSample(0, 0), at(i))
	}
	if r := l.Reading(at(b + 2)); r.Present {
		t.Fatalf("left: %+v", r)
	}
	if got := l.Last(at(b + 2)); got.Limited || !got.PlanBox || got.Mbps != 5 {
		t.Fatalf("the last reading on the chain: %+v", got)
	}
	// Back inside the hold (a download manager's next range, a player's
	// next burst): the box with them at once, the fact after its run.
	back := mbpsSample(2.5, 1)
	back.Throttled = thr(0.1)
	l.Observe(back, at(b+3))
	if r := l.Reading(at(b + 3)); !r.Present || !r.PlanBox || r.Limited {
		t.Fatalf("back within %v of the cap: %+v, want the box", PlanBoxHold, r)
	}
}

// Every open status stream formats its labels on its own goroutine through
// the same cached printers.
func TestFormatNumber_Concurrent(t *testing.T) {
	done := make(chan struct{})
	for g := 0; g < 8; g++ {
		go func(g int) {
			defer func() { done <- struct{}{} }()
			for i := 0; i < 200; i++ {
				lang := []string{"en", "ru", "de", "fr"}[(g+i)%4]
				if FormatNumber(lang, 1.2) == "" {
					t.Error("empty")
				}
			}
		}(g)
	}
	for g := 0; g < 8; g++ {
		<-done
	}
}

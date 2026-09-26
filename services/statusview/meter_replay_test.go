package statusview

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// Real thp events at the plan's cap (testdata/session-stats-at-cap.json:
// prod torrent-http-proxy sha-277778d, an anonymous viewer at 5M, 2026-09-26)
// through the Meter, on their recorded clock. The verdict's thresholds and
// the box's timing are judged here against what a capped stream and a file
// under the cap actually send, not against hand-made samples.

// recordedEvent is one event of a run: when (ms since the page loaded) and
// the Sample it is.
type recordedEvent struct {
	ms int64
	s  Sample
}

func loadRecordedRuns(t *testing.T) map[string][]recordedEvent {
	t.Helper()
	b, err := os.ReadFile("testdata/session-stats-at-cap.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx struct {
		Runs map[string]struct {
			Kind   string           `json:"kind"`
			Events [][]*json.Number `json:"events"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(b, &fx); err != nil {
		t.Fatal(err)
	}
	num := func(n *json.Number) float64 {
		f, err := n.Float64()
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	out := map[string][]recordedEvent{}
	for name, r := range fx.Runs {
		evs := make([]recordedEvent, 0, len(r.Events))
		for i, e := range r.Events {
			s := Sample{WindowSec: 5, BytesPerSec: num(e[1]), Conns: int(num(e[2]))}
			active := num(e[3]) == 1
			s.Active = &active
			if i > 0 {
				// Every event after the stream's first carried the rate.
				s.Rate = "5M"
			}
			if e[4] != nil {
				th := num(e[4])
				s.Throttled = &th
			}
			evs = append(evs, recordedEvent{ms: int64(num(e[0])), s: s})
		}
		out[name+"/"+r.Kind] = evs
	}
	return out
}

// replayed is what the chain showed over a run: the first event with the
// fact and with the box, how many events had each, and the reading at each.
type replayed struct {
	factAt, boxAt int64 // ms, -1 for never
	facts, boxes  int
	readings      []Viewer
}

func replayRun(evs []recordedEvent) replayed {
	var m Meter
	r := replayed{factAt: -1, boxAt: -1}
	base := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	for _, e := range evs {
		now := base.Add(time.Duration(e.ms) * time.Millisecond)
		m.Observe(e.s, now)
		v := m.Reading(now)
		r.readings = append(r.readings, v)
		if v.Limited {
			r.facts++
			if r.factAt < 0 {
				r.factAt = e.ms
			}
		}
		if v.PlanBox {
			r.boxes++
			if r.boxAt < 0 {
				r.boxAt = e.ms
			}
		}
	}
	return r
}

func TestMeter_RecordedCappedStreams(t *testing.T) {
	runs := loadRecordedRuns(t)
	// ms since the page loaded: the fact 3 full-window events into the cap,
	// the box PlanBoxAfter later -- to the event.
	want := map[string][2]int64{
		"stream_start_at_cap/capped":     {22054, 28055},
		"grace_end_at_cap/capped":        {19737, 25737},
		"session_seek_past_grace/capped": {52746, 58745},
	}
	for name, w := range want {
		evs, ok := runs[name]
		if !ok {
			t.Fatalf("no run %s in the fixture", name)
		}
		r := replayRun(evs)
		if r.factAt != w[0] || r.boxAt != w[1] {
			t.Errorf("%s: fact at %d ms, box at %d ms; want %d, %d", name, r.factAt, r.boxAt, w[0], w[1])
		}
		// The box is due planBoxRun-planFactRun events after the fact: no
		// event of a capped stream is lost to the box's "a request open now"
		// (Conns) -- each one it could rise on had one.
		if got := (r.boxAt - r.factAt + 500) / 1000; r.boxAt >= 0 && got != int64(planBoxRun-planFactRun) {
			t.Errorf("%s: box %d events after the fact, want %d", name, got, planBoxRun-planFactRun)
		}
	}
	// The owner's session seek: at the cap to the end, a stall every ~8 s
	// from 60.1 s. The box, once up, stays through all of it -- through the
	// stalls it is what the viewer is sold.
	r := replayRun(runs["session_seek_past_grace/capped"])
	evs := runs["session_seek_past_grace/capped"]
	for i, e := range evs {
		if e.ms >= 58745 && !r.readings[i].PlanBox {
			t.Errorf("the box went at %d ms, at the cap", e.ms)
			break
		}
	}
}

// A file under the cap (Sintel, Vault, 2026-09-26): its buffer fills in
// bursts at the cap -- the limiter binds for a second or two, the window
// reads up to 1.02 of the cap. The pink link may come for an event; the box
// never. With planUseOn at 0.8 the fact came on 4 events, with no use gate at
// all (throttled alone) on 21.
func TestMeter_RecordedFileUnderTheCap(t *testing.T) {
	r := replayRun(loadRecordedRuns(t)["fits_cap/fits"])
	if r.boxes != 0 {
		t.Errorf("the box on %d events (first at %d ms)", r.boxes, r.boxAt)
	}
	if r.facts > 1 {
		t.Errorf("the fact on %d events (first at %d ms), want at most the one burst", r.facts, r.factAt)
	}
}

// The event that sees a download's last request close: thp says active (its
// ends counter moved), conns 0. A box raised there went two seconds later
// with the viewer -- a flash of an 80 px box. It rises only on an event with
// a request open now; one more second at the cap and it does.
func TestPlanState_BoxRisesOnlyWithARequestOpen(t *testing.T) {
	on := capSample(0.9)
	closing := active(Sample{BytesPerSec: capBps, Conns: 0, Rate: "5M", Throttled: thr(0.9)}, true)
	var p planState
	for i := 1; i < planBoxRun; i++ {
		p = p.step(on, true)
	}
	if p = p.step(closing, true); !p.on || p.run != planBoxRun || p.box {
		t.Fatalf("the %d-th event, the request just closed: %+v, want the verdict held and no box", planBoxRun, p)
	}
	if p = p.step(on, true); !p.box {
		t.Fatalf("the next event with a request open: %+v, want the box", p)
	}
	// A download that ends on the event the box was due: no box at all,
	// and the viewer leaves with nothing having flashed. The stream's first
	// event spans nothing, its full windows start at the (thpWindowSec+1)-th:
	// the planBoxRun-th full window at the cap is at s = thpWindowSec +
	// planBoxRun - 1.
	var m Meter
	m.Observe(Sample{}, at(0))
	s := 1
	for ; s < thpWindowSec+planBoxRun-1; s++ {
		m.Observe(on, at(s))
	}
	if v := m.Reading(at(s - 1)); !v.Limited || v.PlanBox {
		t.Fatalf("one event before the box is due: %+v", v)
	}
	m.Observe(closing, at(s))
	if v := m.Reading(at(s)); !v.Limited || v.PlanBox {
		t.Fatalf("the close: %+v, want the fact and no box", v)
	}
	quiet := active(Sample{BytesPerSec: capBps / 2, Rate: "5M", Throttled: thr(0.5)}, false)
	for i := 1; i <= 3; i++ {
		m.Observe(quiet, at(s+i))
		if v := m.Reading(at(s + i)); v.PlanBox {
			t.Fatalf("%d s after the close: %+v, want no box", i, v)
		}
	}
}

// Inside the free grace window, with grace segment tokens carrying the
// session (thp's grace-aware stats, docs/grace_token.md "Session"): the grace
// segments' bytes count -- at the grace rate, far above the tier's -- and
// their limiter's wait does not; rate stays the tier's. That is not the tier
// binding, and nothing is sold: bytes at 3-10× the cap with throttled 0,
// absent, or the playlist polls' trickle of wait never turn the verdict on,
// while the viewer is on the chain with their speed.
func TestMeter_GraceBytesAreNotTheCap(t *testing.T) {
	grace := func(capShare float64, throttled *float64) Sample {
		return active(Sample{WindowSec: 5, BytesPerSec: capShare * capBps, Conns: 1, Rate: "5M", Throttled: throttled}, true)
	}
	// An entry whose only requests so far are grace segments has no rate at
	// all (thp: the playlist on the primary token normally comes first).
	noRate := grace(8, nil)
	noRate.Rate = ""
	var m Meter
	m.Observe(Sample{}, at(0))
	feed := []Sample{grace(3, thr(0)), grace(10, nil), grace(6, thr(0.02)), grace(9.5, thr(0)), noRate}
	for s := 1; s <= 120; s++ {
		m.Observe(feed[s%len(feed)], at(s))
		v := m.Reading(at(s))
		if v.Limited || v.PlanBox {
			t.Fatalf("grace, %d s: %+v -- the cap said inside the grace window", s, v)
		}
		if !v.Present || v.Mbps < 5 {
			t.Fatalf("grace, %d s: %+v, want the viewer on the chain at the grace speed", s, v)
		}
	}
	// The window's last grace seconds with the tier's own segments -- the
	// ones hls.js fetches ahead past the window -- held by the tier: that is
	// the tier binding, and the server says so (the page keeps the box down
	// while its player is still inside the window by movie time:
	// lib/transferStatus.js present, env.inGrace).
	for s := 121; s <= 121+thpWindowSec+planBoxRun; s++ {
		m.Observe(grace(1.4, thr(0.95)), at(s))
	}
	if v := m.Reading(at(121 + thpWindowSec + planBoxRun)); !v.Limited || !v.PlanBox {
		t.Errorf("the tier's segments at the cap after grace: %+v, want the fact and the box", v)
	}
}

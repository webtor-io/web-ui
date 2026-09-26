package resource

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/webtor-io/web-ui/services/statusview"
)

// How fast the page reacts to the plan's cap (owner, 2026-09-25). It used to
// take ~20 s: thp's five-second window ramping up, then fifteen events of
// confirmation. Now the fact -- the pink "5 Mbps · cap" on the viewer's link
// -- comes after three of thp's events at the cap, the plan box after eight
// seconds of a steady cap, and the box, ~80 px tall, stays until ten seconds
// without the cap: a dip does not make the page jump. These run the real
// status handler and statusLoop against a fake thp whose session stream
// sends one event a second, as thp's does; t0 is the first event whose
// window is at the cap -- thp's whole window: its ring for a new stream
// covers 0, 1, ... 4 s over the first five events (ringFill), and nothing
// turns on over those (TestStatusStream_PlanYoungRingIsNotTheCap).

// The owner's numbers, written out here and not read from the package: a
// test that took statusview's constants would follow a change it is meant to
// catch (a negative control that set the fact to one event stayed green).
const (
	ownerFactEvents = 3
	ownerBoxAfter   = 8 * time.Second
	ownerBoxHold    = 10 * time.Second
	// ownerBoxAfterEvents: thp's events, one a second, the box takes -- the
	// first at the cap and eight after it.
	ownerBoxAfterEvents = int(ownerBoxAfter/time.Second) + 1
)

// ringFill is how many of a new stream's events come before the first over
// thp's whole window (window_sec 5): the zero-length one and four filling.
const ringFill = 5

// planIdle is thp's event with no request of the viewer's since the last
// one and nothing in the window.
const planIdle = `{"window_sec":5,"bytes_per_sec":0,"conns":0,"active":false,"rate":"5M"}`

// planEv is thp's event for a viewer downloading at mbps with the limiter
// holding the session for throttled of the window: a request open through
// the second (conns 1, active).
func planEv(mbps, throttled float64) string {
	return fmt.Sprintf(`{"window_sec":5,"bytes_per_sec":%.0f,"conns":1,"active":true,"rate":"5M","throttled":%.2f}`,
		mbps*(1<<20)/8, throttled)
}

// planRun is n of thp's events in a row, each ev.
type planRun struct {
	n  int
	ev string
}

// planFrames lays out thp's events one a second from the stream's open: the
// zero-length window first, then each run of n events.
func planFrames(runs ...planRun) []statFrame {
	evs := []string{`{"window_sec":5,"bytes_per_sec":0,"conns":1,"active":true,"rate":"5M"}`}
	for _, r := range runs {
		for i := 0; i < r.n; i++ {
			evs = append(evs, r.ev)
		}
	}
	return sessEvents(evs...)
}

// planMsg is one status message as it arrived.
type planMsg struct {
	at time.Time
	m  map[string]any
}

func (p planMsg) fact() bool { return get(p.m, "view", "segs", 1, "tone") == "plan" }
func (p planMsg) box() bool  { return get(p.m, "view", "plan", "download", "box") != nil }

// nextMsg reads the next message, failing after d.
func nextMsg(t *testing.T, msgs <-chan map[string]any, d time.Duration, what string) planMsg {
	t.Helper()
	select {
	case m, open := <-msgs:
		if !open {
			t.Fatalf("stream ended before %s", what)
		}
		return planMsg{time.Now(), m}
	case <-time.After(d):
		t.Fatalf("no message within %v waiting for %s", d, what)
	}
	return planMsg{}
}

// within fails unless got is want ± tol.
func within(t *testing.T, what string, got, want, tol time.Duration) {
	t.Helper()
	t.Logf("%s: %v (want %v ± %v)", what, got.Round(time.Millisecond), want, tol)
	if got < want-tol || got > want+tol {
		t.Errorf("%s %v, want %v ± %v", what, got.Round(time.Millisecond), want, tol)
	}
}

// A download reaching the cap at t0: the pink fact within 4 s, the plan box
// at about 8 s, the box through a five-second dip under the cap (the fact
// goes with the dip), and gone about 10 s after the cap was last seen --
// with the viewer still downloading, so it is the box's hold that ends, not
// their presence.
func TestStatusStream_PlanFactThenBox(t *testing.T) {
	const (
		t0      = ringFill + 1 // the first event at the cap
		dipFrom = t0 + 12      // five events under it
		lastCap = dipFrom + 8  // the last event at the cap
	)
	atCap, under := planEv(5, 0.8), planEv(2.5, 0.1)
	frames := planFrames(
		planRun{ringFill, under}, // 1-5: on the chain, under the cap; thp's ring full at 5
		planRun{12, atCap},       // 6-17
		planRun{5, under},        // 18-22: the dip
		planRun{4, atCap},        // 23-26
		planRun{14, under},       // 27-40: downloading on, under the cap
	)
	node := newFakeNode(t, nil, func(n *fakeNode) { n.sessFrames = frames })
	h := &Handler{api: testAPI(t, node.srv), offers: liveOffers()}
	srv := statusServer(t, h, "free", "5M")
	// Parallel only past the setup: statusServer's i18n.New writes the
	// package's globals.
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	msgs, _, _ := sseStream(ctx, t, srv.URL+"/"+ssHash+"/status?_csrf=tok&session=1")

	// The fact: pink, and nothing sold yet -- not even the box's line.
	var m planMsg
	for m = nextMsg(t, msgs, 20*time.Second, "the fact"); !m.fact(); m = nextMsg(t, msgs, 20*time.Second, "the fact") {
		if m.box() || get(m.m, "view", "plan") != nil {
			t.Fatalf("a plan before the fact: %v", m.m["view"])
		}
	}
	start := node.sessFrameAt(t0)
	if start.IsZero() {
		t.Fatalf("the fact before the cap: %v", m.m["view"])
	}
	if third := node.sessFrameAt(t0 + ownerFactEvents - 1); third.IsZero() || m.at.Before(third) {
		t.Errorf("the fact %v after t0, before the %d-th event at the cap", m.at.Sub(start).Round(time.Millisecond), ownerFactEvents)
	}
	if after := m.at.Sub(start); after > 4*time.Second {
		t.Errorf("the fact %v after t0, want within 4 s", after.Round(time.Millisecond))
	}
	t.Logf("the fact: %v after t0", m.at.Sub(start).Round(time.Millisecond))
	if get(m.m, "view", "key") != statusview.KeyCachedTier || get(m.m, "view", "segs", 1, "speed") != "5\u00a0Mbps" ||
		get(m.m, "view", "segs", 1, "note") != "cap" || get(m.m, "view", "plan", "fact") == nil {
		t.Errorf("the fact: %v", m.m["view"])
	}
	for _, v := range []string{"download", "stream"} {
		if p, _ := get(m.m, "view", "plan", v).(map[string]any); len(p) != 0 {
			t.Errorf("the fact: the %s variant already %v", v, p)
		}
	}

	// The box: about 8 s after t0, both variants; nothing sold before it.
	for m = nextMsg(t, msgs, 12*time.Second, "the box"); !m.box(); m = nextMsg(t, msgs, 12*time.Second, "the box") {
		if p, _ := get(m.m, "view", "plan", "download").(map[string]any); len(p) != 0 {
			t.Fatalf("the download variant before the box: %v", p)
		}
	}
	within(t, "the box after t0", m.at.Sub(start), ownerBoxAfter, time.Second)
	if get(m.m, "view", "plan", "download", "box", "cta", "url") != "/trial?from=status-bar" || get(m.m, "view", "plan", "stream", "box") == nil {
		t.Errorf("the box: %v", get(m.m, "view", "plan"))
	}

	// Through the dip and the second run at the cap, the box stays; the
	// fact goes with the dip (else the dip proves nothing).
	dipSeen := false
	for m = nextMsg(t, msgs, 15*time.Second, "the box's end"); m.box(); m = nextMsg(t, msgs, 15*time.Second, "the box's end") {
		if !m.fact() && get(m.m, "view", "segs", 1, "show") == true && get(m.m, "view", "key") == statusview.KeyCachedTier {
			dipSeen = true
		}
	}
	if !dipSeen {
		t.Error("no message with the box and without the fact: the dip never read under the cap")
	}
	last := node.sessFrameAt(lastCap)
	if last.IsZero() {
		t.Fatalf("the box went before the second run at the cap ended (dip from %v): %v",
			node.sessFrameAt(dipFrom).Sub(start).Round(time.Millisecond), m.m["view"])
	}
	within(t, "the box's end after the cap was last seen", m.at.Sub(last), ownerBoxHold, time.Second)
	// Still downloading: it is the hold that ended, not the viewer.
	if get(m.m, "view", "nodes", 2, "show") != true || get(m.m, "view", "key") != statusview.KeyCachedFlow || get(m.m, "view", "plan") != nil {
		t.Errorf("after the box: %v", m.m["view"])
	}
}

// planTail is thp's event after the viewer's requests closed: none open,
// active only on the event that sees the close, the window still holding
// the bytes and the waiting.
func planTail(mbps, throttled float64, active bool) string {
	return fmt.Sprintf(`{"window_sec":5,"bytes_per_sec":%.0f,"conns":0,"active":%t,"rate":"5M","throttled":%.2f}`,
		mbps*(1<<20)/8, active, throttled)
}

// watchNoBox reads the stream until 25 s have passed, failing on any plan
// box; it reports whether the fact showed, and whether the viewer left
// after it (the badge).
func watchNoBox(t *testing.T, msgs <-chan map[string]any, what string) (fact, left bool) {
	t.Helper()
	deadline := time.After(25 * time.Second)
	for {
		select {
		case mm, open := <-msgs:
			if !open {
				t.Fatal("the stream ended")
			}
			m := planMsg{time.Now(), mm}
			if m.box() || get(m.m, "view", "playing", "plan", "download", "box") != nil {
				t.Fatalf("a plan box for %s: %v", what, m.m["view"])
			}
			if m.fact() {
				fact = true
			}
			if fact && get(m.m, "view", "mode") == statusview.ModeBadge {
				left = true
			}
		case <-deadline:
			return fact, left
		}
	}
}

// A small file: five seconds at the cap, then its requests close. The pink
// fact, and no plan box at any moment -- through the window's tail, the
// viewer leaving and the seconds after. The page was open a while before
// it: thp's ring for the stream is full.
func TestStatusStream_PlanBurstShowsTheFactOnly(t *testing.T) {
	const burst = 5
	frames := planFrames(
		planRun{ringFill, planIdle},
		planRun{burst, planEv(5, 0.8)},
		planRun{1, planTail(4, 0.64, true)}, // thp sees the close
		planRun{1, planTail(3, 0.48, false)},
		planRun{1, planTail(2, 0.32, false)},
		planRun{1, planTail(1, 0.16, false)},
		planRun{6, planIdle},
	)
	node := newFakeNode(t, nil, func(n *fakeNode) { n.sessFrames = frames })
	h := &Handler{api: testAPI(t, node.srv), offers: liveOffers()}
	srv := statusServer(t, h, "free", "5M")
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	msgs, _, _ := sseStream(ctx, t, srv.URL+"/"+ssHash+"/status?_csrf=tok&session=1")

	fact, left := watchNoBox(t, msgs, fmt.Sprintf("a %d s burst", burst))
	if !fact {
		t.Error("the burst at the cap never showed the fact")
	}
	if !left {
		t.Error("the viewer never left")
	}
	if node.sessFrameAt(len(frames) - 1).IsZero() {
		t.Error("the fake did not play all its events")
	}
}

// A download at the cap that closes one event short of the box: the event
// that sees the close (still active) brings the run to eight seconds, the
// window's tail after it -- no request since, use 0.76, throttled 0.72 --
// would have been the ninth and held the verdict (review F2, 2026-09-26).
// The ~80 px box must not come up for the second before the viewer leaves.
func TestStatusStream_PlanTailRaisesNoBox(t *testing.T) {
	frames := planFrames(
		planRun{ringFill, planIdle},
		planRun{ownerBoxAfterEvents - 2, planEv(5, 0.9)}, // at the cap, requests open
		planRun{1, planTail(4.8, 0.91, true)},            // the close
		planRun{1, planTail(3.8, 0.72, false)},           // the tail: held only
		planRun{1, planTail(2.8, 0.53, false)},
		planRun{1, planTail(1.8, 0.34, false)},
		planRun{6, planIdle},
	)
	node := newFakeNode(t, nil, func(n *fakeNode) { n.sessFrames = frames })
	h := &Handler{api: testAPI(t, node.srv), offers: liveOffers()}
	srv := statusServer(t, h, "free", "5M")
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	msgs, _, _ := sseStream(ctx, t, srv.URL+"/"+ssHash+"/status?_csrf=tok&session=1")

	fact, left := watchNoBox(t, msgs, "a download that closed before the box")
	if !fact {
		t.Error("the download at the cap never showed the fact")
	}
	if !left {
		t.Error("the viewer never left")
	}
	if node.sessFrameAt(len(frames) - 1).IsZero() {
		t.Error("the fake did not play all its events")
	}
}

// A new stream's first events average over the one, two, three, four
// seconds thp's ring covers so far: the page's HLS player fetching a 3.5
// Mbps file's 4 s segments at a 5 Mbps cap reads the cap there and
// 0.56-0.76 of it over the full window (review F1, 2026-09-26, thp's ring
// modelled). Every open of the stream -- a reopen, a web-ui rollout
// reconnecting every page -- would light the pink link, the cap tag and the
// fact line for a few seconds and take them down again. Nothing of the plan
// is said, the viewer is on the chain.
func TestStatusStream_PlanYoungRingIsNotTheCap(t *testing.T) {
	young := func(use, throttled float64) string {
		return fmt.Sprintf(`{"window_sec":5,"bytes_per_sec":%.0f,"conns":0,"active":true,"rate":"5M","throttled":%.2f}`,
			use*5*(1<<20)/8, throttled)
	}
	frames := sessEvents(
		`{"window_sec":5,"bytes_per_sec":0,"conns":0,"active":true,"rate":"5M"}`,
		young(1.00, 1.00), young(1.00, 1.00), young(0.93, 0.93), young(0.70, 0.70), // spans 1-4 s
		young(0.76, 0.76), young(0.56, 0.56), young(0.66, 0.66), young(0.72, 0.72), // full
		young(0.60, 0.60), young(0.76, 0.76), young(0.56, 0.56), young(0.66, 0.66),
	)
	node := newFakeNode(t, nil, func(n *fakeNode) { n.sessFrames = frames })
	h := &Handler{api: testAPI(t, node.srv), offers: liveOffers()}
	srv := statusServer(t, h, "free", "5M")
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	msgs, _, _ := sseStream(ctx, t, srv.URL+"/"+ssHash+"/status?_csrf=tok&session=1")

	drawn := false
	deadline := time.After(time.Duration(len(frames)+2) * time.Second)
	for {
		select {
		case m, open := <-msgs:
			if !open {
				t.Fatal("the stream ended")
			}
			if get(m, "view", "segs", 1, "tone") == "plan" || get(m, "view", "plan") != nil || get(m, "view", "key") == statusview.KeyCachedTier {
				t.Fatalf("the plan from a young ring: %v", m["view"])
			}
			if get(m, "view", "segs", 1, "show") == true {
				drawn = true
			}
		case <-deadline:
			if !drawn {
				t.Error("the viewer was never on the chain: the test proves nothing")
			}
			if node.sessFrameAt(len(frames) - 1).IsZero() {
				t.Error("the fake did not play all its events")
			}
			return
		}
	}
}

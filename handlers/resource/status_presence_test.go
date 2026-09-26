package resource

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/webtor-io/web-ui/services/statusview"
)

// The viewer is on the chain by the proxy's exact connection signal, not by
// their speed (owner, 2026-09-25): thp says whether a request of theirs was
// open since its previous event (active; conns, the ones open at the event),
// and web-ui keeps them until thp has seen none for
// statusview.PresenceDebounce. Measured the same day: conns went 1→0 within 1.2 s of a
// client's abort, yet the page drew "you" for ~25 s more -- the five-second
// window's tail, the meter's ten-second speed hold, and a chain's hold on
// top. These run the real status handler and statusLoop against a fake thp
// whose session stream sends one event a second, as thp's does.

// held is one of thp's session-stats events for requests that each stay
// open across events (a download): a speed over the window and the requests
// open at the event.
type held struct {
	mbps  float64
	conns int
}

// heldEvents are thp's events for those requests, with "active" as thp
// derives it (statsRing.event): a request open at the event, or one that
// ended since the previous event -- its ends counter moved. So the first
// event with conns 0 still says active: that is where thp sees the close,
// and conns 1→0 with active false is an event thp never sends.
func heldEvents(evs ...held) []string {
	out := make([]string, len(evs))
	prev := 0
	for i, e := range evs {
		out[i] = fmt.Sprintf(`{"window_sec":5,"bytes_per_sec":%.0f,"conns":%d,"rate":"5M","active":%t}`,
			e.mbps*(1<<20)/8, e.conns, e.conns > 0 || prev > 0)
		prev = e.conns
	}
	return out
}

// sessEvents lays events out one a second from the stream's open; the first
// is thp's zero-length window.
func sessEvents(evs ...string) []statFrame {
	out := make([]statFrame, len(evs))
	for i, e := range evs {
		out[i] = statFrame{time.Duration(i) * time.Second, e}
	}
	return out
}

// A download manager aborts a download of cached content -- nothing else
// moves, the swarm is idle. thp's count drops to zero at once and its next
// event reports the close (active, conns 0); its window still carries the
// bytes for five seconds. The viewer leaves the chain once thp has seen no
// request of theirs for PresenceDebounce -- the second event without one --
// the window's bytes notwithstanding, and with nothing else moving the badge
// comes at once: 2-3 s after the close, not ~25 s, and not a second later
// (the point sample's third event, counted on "active", took 3-4 s). The
// server also sends the view with them still on it, for a page whose own
// player is playing (view.playing).
func TestStatusStream_ViewerLeavesWithTheirConnection(t *testing.T) {
	const drop = 5 // the first event with no request open: the close
	frames := sessEvents(heldEvents(
		held{0, 1}, // the zero-length window
		held{24, 1}, held{24, 1}, held{24, 1}, held{24, 1},
		// Aborted: no request open, the window's tail decaying.
		held{19, 0}, held{14, 0}, held{10, 0}, held{5, 0},
		held{0, 0}, held{0, 0}, held{0, 0}, held{0, 0},
	)...)
	node := newFakeNode(t, nil, func(n *fakeNode) { n.sessFrames = frames })
	h := &Handler{api: testAPI(t, node.srv), offers: liveOffers()}
	srv := statusServer(t, h, "free", "5M")
	// Parallel only past the setup: statusServer's i18n.New writes the
	// package's globals.
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	msgs, _, _ := sseStream(ctx, t, srv.URL+"/"+ssHash+"/status?_csrf=tok&session=1")

	m := until(t, msgs, 8*time.Second, "the viewer downloading", func(m map[string]any) bool {
		return get(m, "view", "nodes", 2, "show") == true
	})
	if get(m, "view", "key") != statusview.KeyCachedFlow || get(m, "view", "mode") != statusview.ModeChain {
		t.Errorf("downloading: %v", m["view"])
	}
	m = until(t, msgs, 10*time.Second, "the badge", func(m map[string]any) bool {
		return get(m, "view", "mode") == statusview.ModeBadge
	})
	at := time.Now()
	dropAt := node.sessFrameAt(drop)
	if dropAt.IsZero() {
		t.Fatalf("the badge before the viewer's requests closed: %v", m["view"])
	}
	after := at.Sub(dropAt)
	t.Logf("the badge %v after the event that saw the close", after.Round(time.Millisecond))
	if after > 3*time.Second {
		t.Errorf("the badge %v after the requests closed, want within 3 s", after.Round(time.Millisecond))
	}
	// The debounce, not the speed: the close and one event without a
	// request are still the viewer's, the second without one ends it --
	// with 10 Mbps still in the window -- and not the third.
	if second := node.sessFrameAt(drop + presenceSamplesAfter); second.IsZero() || at.Before(second) {
		t.Errorf("gone %v after the close, inside the debounce", after.Round(time.Millisecond))
	}
	if third := node.sessFrameAt(drop + presenceSamplesAfter + 1); !third.IsZero() && third.Before(at) {
		t.Errorf("gone only at the third event without a request, %v after the close", after.Round(time.Millisecond))
	}
	if get(m, "view", "key") != statusview.KeyCached || get(m, "view", "nodes", 2, "show") != false || get(m, "view", "plan") != nil {
		t.Errorf("the badge: %v", m["view"])
	}
	// The page's own player, if it plays, keeps them: the view with them on
	// the chain at their last reading.
	if get(m, "view", "playing", "key") != statusview.KeyCachedFlow || get(m, "view", "playing", "nodes", 2, "show") != true {
		t.Errorf("no view for the page's player: %v", get(m, "view", "playing"))
	}
}

// HLS fetches segments in bursts, and a download manager swaps one range
// request for the next: thp's count reads zero for a sample or two while the
// viewer is still there. With no player on the page to say otherwise, the
// debounce alone keeps them through gaps under PresenceDebounce -- and a
// longer gap drops them, at the third event with none open (the first of
// which thp reports as the close, active), the window's bytes or not. A
// request open with no bytes yet is on the chain too.
func TestStatusStream_ConnectionGapsUnderTheDebounce(t *testing.T) {
	const long = 9 // the first event of the long gap: the close
	frames := sessEvents(heldEvents(
		held{0, 1}, // the zero-length window
		held{0, 1}, // a request open, no bytes yet
		held{6, 1},
		held{6, 0}, // a one-event gap
		held{6, 1},
		held{6, 0}, held{5, 0}, // a two-event gap
		held{6, 1},
		held{6, 1},
		held{6, 0}, held{5, 0}, held{4, 0}, held{3, 0}, held{0, 0}, held{0, 0},
	)...)
	node := newFakeNode(t, nil, func(n *fakeNode) { n.sessFrames = frames })
	h := &Handler{api: testAPI(t, node.srv), offers: liveOffers()}
	srv := statusServer(t, h, "free", "5M")
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	msgs, _, _ := sseStream(ctx, t, srv.URL+"/"+ssHash+"/status?_csrf=tok&session=1")

	m := until(t, msgs, 8*time.Second, "the viewer on the chain", func(m map[string]any) bool {
		return get(m, "view", "nodes", 2, "show") == true
	})
	// Their first sample had no bytes: on the chain all the same, no number.
	if sp, _ := get(m, "view", "segs", 1, "speed").(string); !strings.Contains(sp, "—") {
		t.Errorf("connected, no bytes yet: speed %q, want a dash (%v)", sp, m["view"])
	}
	deadline := time.After(20 * time.Second)
	for {
		select {
		case m = <-msgs:
		case <-deadline:
			t.Fatal("the viewer never left")
		}
		if m == nil || m["view"] == nil || get(m, "view", "nodes", 2, "show") == true {
			continue
		}
		// Gone. Only in the long gap, at its third event with none open --
		// not at the fourth.
		at := time.Now()
		third := node.sessFrameAt(long + presenceSamplesAfter)
		if third.IsZero() || at.Before(third) {
			t.Fatalf("dropped in a gap under the debounce: %v (long gap's third event out: %v)", m["view"], !third.IsZero())
		}
		if fourth := node.sessFrameAt(long + presenceSamplesAfter + 1); !fourth.IsZero() && fourth.Before(at) {
			t.Errorf("dropped %v after the long gap's third event, at its fourth", at.Sub(third).Round(time.Millisecond))
		}
		if get(m, "view", "mode") != statusview.ModeBadge || get(m, "view", "key") != statusview.KeyCached {
			t.Errorf("gone, nothing else moving: %v", m["view"])
		}
		return
	}
}

// presenceSamplesAfter: the drop comes this many events after the event
// that saw the close -- the second without a request (statusview.
// PresenceDebounce at thp's one a second, each covering the second before
// it).
const presenceSamplesAfter = int(statusview.PresenceDebounce / time.Second)

// hlsEvent is thp's event as it read live on 2026-09-25 for a paid viewer
// (no limiter, no rate) streaming in the page's player: each HLS segment
// came from the transcoder in well under a second, so conns -- a point
// sample, once a second -- read 0 in every event at 1.1-2.2 MB/s. active is
// thp's "active" (a request open at any moment since its last event); nil
// is a thp from before the field, which does not send it.
func hlsEvent(bytesPerSec float64, active *bool) string {
	if active == nil {
		return fmt.Sprintf(`{"window_sec":5,"bytes_per_sec":%.0f,"conns":0}`, bytesPerSec)
	}
	return fmt.Sprintf(`{"window_sec":5,"bytes_per_sec":%.0f,"conns":0,"active":%t}`, bytesPerSec, *active)
}

// The owner's bug: streaming in the page's player, "Вы" never came -- thp
// read conns 0 in every event, and the page's player had no last reading
// to keep them by. With thp's "active" the viewer is on the chain while the
// player fetches, leaves once thp has seen no request of theirs for
// PresenceDebounce after the fetches stop (its buffer full), and the view for
// the page's playing player (view.playing) has their reading. A thp without
// the field falls back to the window's bytes: the same, with the window's
// tail before the debounce.
func TestStatusStream_PagePlayerHLSViewerIsOnTheChain(t *testing.T) {
	yes, no := true, false
	for _, tc := range []struct {
		name string
		yes  *bool
		no   *bool
		// gone: the event the viewer leaves at -- the second without a
		// request (two seconds with none open), or with the old thp the
		// third with an empty window (point samples).
		gone int
	}{
		{"thp with active", &yes, &no, 8},
		{"thp without active", nil, nil, 13},
	} {
		t.Run(tc.name, func(t *testing.T) {
			evs := []string{hlsEvent(0, tc.yes)} // the zero-length window
			for _, bps := range []float64{1.1e6, 2.2e6, 1.6e6, 1.1e6, 1.9e6, 1.4e6} {
				evs = append(evs, hlsEvent(bps, tc.yes))
			}
			// The buffer is full, the player stops fetching (event 7 on);
			// the window empties by event 11.
			for _, bps := range []float64{1.2e6, 0.8e6, 0.4e6, 0.1e6, 0, 0, 0, 0, 0, 0} {
				evs = append(evs, hlsEvent(bps, tc.no))
			}
			node := newFakeNode(t, nil, func(n *fakeNode) { n.sessFrames = sessEvents(evs...) })
			h := &Handler{api: testAPI(t, node.srv), offers: liveOffers()}
			srv := statusServer(t, h, "silver", "")
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
			defer cancel()
			msgs, _, _ := sseStream(ctx, t, srv.URL+"/"+ssHash+"/status?_csrf=tok&session=1")

			m := until(t, msgs, 8*time.Second, "the viewer on the chain", func(m map[string]any) bool {
				return get(m, "view", "nodes", 2, "show") == true
			})
			if get(m, "view", "key") != statusview.KeyCachedFlow || get(m, "view", "mode") != statusview.ModeChain {
				t.Errorf("streaming: %v", m["view"])
			}
			if node.sessFrameAt(7) != (time.Time{}) {
				t.Errorf("on the chain only after the fetches stopped")
			}
			m = until(t, msgs, 5*time.Second, "their speed", func(m map[string]any) bool {
				sp, _ := get(m, "view", "segs", 1, "speed").(string)
				return strings.ContainsAny(sp, "123456789")
			})
			if get(m, "view", "nodes", 2, "show") != true {
				t.Errorf("a speed without the viewer: %v", m["view"])
			}
			m = until(t, msgs, 20*time.Second, "the badge", func(m map[string]any) bool {
				return get(m, "view", "mode") == statusview.ModeBadge
			})
			at := time.Now()
			gone := node.sessFrameAt(tc.gone)
			if gone.IsZero() {
				t.Fatalf("the badge before event %d: %v", tc.gone, m["view"])
			}
			if next := node.sessFrameAt(tc.gone + 1); !next.IsZero() && next.Before(at) {
				t.Errorf("the badge %v after event %d, at the next one, want at it", at.Sub(gone).Round(time.Millisecond), tc.gone)
			}
			if get(m, "view", "key") != statusview.KeyCached || get(m, "view", "nodes", 2, "show") != false {
				t.Errorf("the badge: %v", m["view"])
			}
			// The page's player, still playing on its buffer, keeps them
			// on the chain at their last reading.
			sp, _ := get(m, "view", "playing", "segs", 1, "speed").(string)
			if get(m, "view", "playing", "nodes", 2, "show") != true || !strings.ContainsAny(sp, "123456789") {
				t.Errorf("no reading for the page's player: %v", get(m, "view", "playing"))
			}
		})
	}
}

package resource

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/statusview"
)

// The status stream's view keeps the swarm on the chain through the gaps
// between its pieces, drawn as it last moved: the chain gives way to the
// badge only HoldFor after the swarm last moved, and comes back the moment
// it moves again. The hold is the stream's own (viewEnv), so one page's
// gap is not another's.
func TestViewEnv_HoldsTheSwarmThroughAGap(t *testing.T) {
	env := &viewEnv{lang: "ru", loc: i18n.New(os.DirFS("../../locales")).Localizer("ru"), tier: "free", withView: true}
	moving := func() *TorrentStatus {
		return &TorrentStatus{State: "caching", Progress: 43, Seeders: 14, Rate: txMbps(38), swarmKnown: true}
	}
	// still is the loop's status once the bytes stopped: the smoothed rate
	// still decaying (swarmStill hides it from the view), then the pause.
	still := func(paused bool) *TorrentStatus {
		return &TorrentStatus{State: "caching", Progress: 43, Seeders: 14, Rate: txMbps(9), swarmStill: true, Paused: paused, swarmKnown: true}
	}
	t0 := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	const moved = "Рой 14 сидов [flow+ 38 Мбит/с] Кэш 43%"
	steps := []struct {
		st    *TorrentStatus
		at    time.Duration
		key   string
		mode  string
		chain string
	}{
		{still(true), 0, statusview.KeyPaused, statusview.ModeBadge, ""},
		{moving(), time.Second, statusview.KeyCachingOnly, statusview.ModeChain, moved},
		{still(false), 2 * time.Second, statusview.KeyCachingOnly, statusview.ModeChain, moved},
		{still(true), 7 * time.Second, statusview.KeyCachingOnly, statusview.ModeChain, moved},
		{still(true), time.Second + statusview.HoldFor - time.Millisecond, statusview.KeyCachingOnly, statusview.ModeChain, moved},
		{still(true), time.Second + statusview.HoldFor, statusview.KeyPaused, statusview.ModeBadge, ""},
		{moving(), time.Second + statusview.HoldFor + time.Second, statusview.KeyCachingOnly, statusview.ModeChain, moved},
	}
	for i, s := range steps {
		env.present(s.st, statusview.Viewer{}, statusview.Viewer{}, 0, 0, t0.Add(s.at))
		v := s.st.View
		if v.Key != s.key || v.Mode != s.mode {
			t.Errorf("step %d (+%v): %s %s, want %s %s", i, s.at, v.Key, v.Mode, s.key, s.mode)
		}
		if s.chain != "" {
			if got := txChain(v); got != s.chain {
				t.Errorf("step %d (+%v): chain %s, want %s", i, s.at, got, s.chain)
			}
		}
	}
	// A second stream starts with nothing held.
	other := &viewEnv{lang: "ru", loc: env.loc, tier: "free", withView: true}
	st := still(true)
	other.present(st, statusview.Viewer{}, statusview.Viewer{}, 0, 0, t0.Add(3*time.Second))
	if st.View.Mode != statusview.ModeBadge {
		t.Errorf("another stream inherited the hold: %s", st.View.Mode)
	}
}

// A still swarm has no rate in the view, whatever the smoothed rate says:
// the chain moves while the bytes arrive, not while their average decays.
func TestViewTorrent_StillSwarmHasNoRate(t *testing.T) {
	st := &TorrentStatus{State: "caching", Rate: txMbps(9), swarmStill: true}
	if r := st.viewTorrent(false).RateBps; r != 0 {
		t.Errorf("still: rate %v", r)
	}
	st.swarmStill = false
	if r := st.viewTorrent(false).RateBps; r != txMbps(9) {
		t.Errorf("moving: rate %v", r)
	}
	// The JSON's rate is the smoothed one either way (the Vault dashboard).
	if st.Rate != txMbps(9) {
		t.Error("the status's own rate changed")
	}
}

// txChain is the chain as the design reads it (services/statusview's test
// helper, restated): nodes by name and value, segments as [tone(+) speed].
func txChain(v *statusview.View) string {
	var parts []string
	for i := 0; i < 3; i++ {
		if n := v.Nodes[i]; n.Show {
			s := n.Name
			if n.Value != "" {
				s += " " + n.Value
			}
			parts = append(parts, s)
		}
		if i < 2 {
			if g := v.Segs[i]; g.Show {
				s := "[" + g.Tone
				if g.On {
					s += "+"
				}
				if g.Speed != "" {
					s += " " + g.Speed
				}
				if g.Note != "" {
					s += " · " + g.Note
				}
				parts = append(parts, s+"]")
			}
		}
	}
	return strings.ReplaceAll(strings.Join(parts, " "), "\u00a0", " ")
}

// The viewer's stream to thp is lost mid-download (a thp pod rotation, an
// ingress reload: every stream on a node at once) and reopened with
// backoff; the reopened stream's first event carries no speed. The bytes
// keep flowing all along, and the chain does not blink to the badge and
// back: the viewer is held like the swarm is.
func TestViewEnv_HoldsTheViewerThroughALostStream(t *testing.T) {
	for _, jit := range []float64{0, 0.5, 0.999} {
		ctx := context.Background()
		env := &viewEnv{lang: "ru", loc: i18n.New(os.DirFS("../../locales")).Localizer("ru"), tier: "free", withView: true}
		w, _, clk := newTestWatch(sessionOpen{ch: make(chan api.SessionStatsData)}, sessionOpen{ch: make(chan api.SessionStatsData)})
		w.jitter = func() float64 { return jit }
		w.start(ctx, testTarget)
		w.opened(ctx, recv(t, w))
		// The watch's tokens expire on the wall clock (countingMint): the
		// test's clock starts there, or a loss would read as a rotation.
		t0 := time.Now()
		ev := api.SessionStatsData{BytesPerSec: 24.0 * (1 << 20) / 8, Conns: 1}
		reopenAt := 5*time.Second + retryDelay(1, jit)
		var modes []string
		for ms := 0; ms <= 16000; ms += 250 {
			at := time.Duration(ms) * time.Millisecond
			now := t0.Add(at)
			switch {
			case at <= 5*time.Second && ms%1000 == 0:
				w.event(ctx, ev, true, now)
			case at == 5*time.Second+250*time.Millisecond:
				w.event(ctx, api.SessionStatsData{}, false, now) // lost
			}
			if at >= reopenAt && at < reopenAt+250*time.Millisecond {
				clk.fire()
				w.opened(ctx, recv(t, w))
				w.event(ctx, ev, true, now) // the new stream's first, skipped
			}
			if at > reopenAt+250*time.Millisecond && (at-reopenAt)%time.Second < 250*time.Millisecond {
				w.event(ctx, ev, true, now)
			}
			if at < 2*time.Second {
				continue // the first reading takes two events
			}
			st := &TorrentStatus{State: "cached", Progress: 100}
			env.present(st, env.viewer(w, now), statusview.Viewer{}, 0, 0, now)
			if len(modes) == 0 || modes[len(modes)-1] != st.View.Mode {
				modes = append(modes, st.View.Mode)
			}
			if st.View.Mode != statusview.ModeChain {
				t.Errorf("jitter %v, +%v: %s %s while the bytes flow", jit, at, st.View.Key, st.View.Mode)
				break
			}
		}
		if len(modes) != 1 {
			t.Errorf("jitter %v: modes %v", jit, modes)
		}
	}
}

// Held only while a reopen is on its way, and only for HoldFor: a thp whose
// retries ran out, or that gave a final answer, leaves the viewer unknown --
// not drawn -- at once.
func TestViewEnv_ViewerHoldEndsWhenNothingIsOnItsWay(t *testing.T) {
	ctx := context.Background()
	env := &viewEnv{lang: "ru", loc: i18n.New(os.DirFS("../../locales")).Localizer("ru"), tier: "free", withView: true}
	w, _, _ := newTestWatch(sessionOpen{ch: make(chan api.SessionStatsData)})
	w.start(ctx, testTarget)
	w.opened(ctx, recv(t, w))
	t0 := time.Now()
	ev := api.SessionStatsData{BytesPerSec: 24.0 * (1 << 20) / 8, Conns: 1}
	w.event(ctx, ev, true, t0)
	w.event(ctx, ev, true, t0.Add(time.Second))
	if v := env.viewer(w, t0.Add(time.Second)); !v.Known || v.Mbps == 0 {
		t.Fatalf("flowing: %+v", v)
	}
	w.event(ctx, api.SessionStatsData{}, false, t0.Add(2*time.Second)) // lost, a retry scheduled
	if !w.reconnecting() {
		t.Fatal("a retry is on its way: reconnecting")
	}
	if v := env.viewer(w, t0.Add(6*time.Second)); !v.Known {
		t.Errorf("lost, inside the hold: unknown %+v", v)
	}
	if v := env.viewer(w, t0.Add(time.Second+statusview.HoldFor)); v.Known {
		t.Errorf("lost past the hold: still drawn %+v", v)
	}
	w.gaveUp = true
	if w.reconnecting() {
		t.Error("given up: nothing on its way")
	}
	env2 := &viewEnv{lang: "ru", loc: env.loc, tier: "free", withView: true}
	env2.hold.Viewer(statusview.Viewer{Known: true, Present: true, Mbps: 24}, false, t0.Add(time.Second))
	if v := env2.viewer(w, t0.Add(5*time.Second)); v.Known {
		t.Errorf("given up: drawn %+v", v)
	}
}

// The page's own player plays a cached film across a thp reopen. Between
// two HLS segments thp counts no request of theirs open and the server's
// view drops the viewer; the page keeps them on the chain by the view sent
// next to it (View.Playing, from their last reading on the chain). A pod
// rotation then cuts the stream -- held through the gap (Hold.Viewer) --
// and the reopened stream's first counted samples fall between segments
// too. The last reading survives the reopen (Meter.StreamOpened), so the
// page's player still keeps the viewer on the chain: never the badge while
// the film plays. It used to read the badge until a sample happened to
// catch a segment request open.
func TestViewEnv_PagePlayerKeepsTheViewerAcrossAReopenInAnHLSGap(t *testing.T) {
	ctx := context.Background()
	env := &viewEnv{lang: "ru", loc: i18n.New(os.DirFS("../../locales")).Localizer("ru"), tier: "free", withView: true}
	w, _, clk := newTestWatch(sessionOpen{ch: make(chan api.SessionStatsData)}, sessionOpen{ch: make(chan api.SessionStatsData)})
	w.start(ctx, testTarget)
	w.opened(ctx, recv(t, w))
	// The watch's tokens expire on the wall clock (countingMint): the
	// test's clock starts there, or the loss would read as a rotation.
	t0 := time.Now()
	sec := func(s float64) time.Time { return t0.Add(time.Duration(s * float64(time.Second))) }
	// thp says whether a request was open since its last event ("active"):
	// between two segments it says no, the window's bytes decaying.
	yes, no := true, false
	segment := api.SessionStatsData{BytesPerSec: 24.0 * (1 << 20) / 8, Conns: 1, Active: &yes}
	gap := func(share float64) api.SessionStatsData {
		return api.SessionStatsData{BytesPerSec: segment.BytesPerSec * share, Active: &no}
	}
	// shown is what the page draws while its player streams (status.js:
	// transferStatus.playing -- the server's alternative when it sent one).
	shown := func(what string, now time.Time) {
		t.Helper()
		st := &TorrentStatus{State: "cached", Progress: 100}
		env.present(st, env.viewer(w, now), env.last(w, now), 0, 0, now)
		v := st.View
		if v.Playing != nil {
			v = v.Playing
		}
		if v.Mode != statusview.ModeChain || !v.Nodes[2].Show {
			t.Errorf("%s (+%v): the page's player plays, yet %s %s", what, now.Sub(t0), v.Key, v.Mode)
		}
	}
	w.event(ctx, segment, true, sec(0)) // the stream's first: skipped
	for s := 1; s <= 4; s++ {
		w.event(ctx, segment, true, sec(float64(s)))
		shown("a segment", sec(float64(s)))
	}
	for i, share := range []float64{0.8, 0.6, 0.4, 0.2} {
		w.event(ctx, gap(share), true, sec(float64(5+i)))
		shown("between two segments", sec(float64(5+i)))
	}
	w.event(ctx, api.SessionStatsData{}, false, sec(8.5)) // lost: a reopen on its way
	for _, s := range []float64{9, 10, 11, 12} {
		shown("reconnecting", sec(s))
	}
	clk.fire()
	w.opened(ctx, recv(t, w))
	w.event(ctx, gap(0), true, sec(12.2)) // the reopened stream's first: skipped
	shown("reopened", sec(12.2))
	for _, s := range []float64{13.2, 14.2, 15.2, 16.2, 20.2} {
		w.event(ctx, gap(0), true, sec(s))
		shown("reopened, between two segments", sec(s))
	}
	// Nobody's player: the server's own view, the badge -- the last
	// reading is for the page's player only.
	st := &TorrentStatus{State: "cached", Progress: 100}
	env.present(st, env.viewer(w, sec(20.2)), env.last(w, sec(20.2)), 0, 0, sec(20.2))
	if st.View.Mode != statusview.ModeBadge || st.View.Playing == nil {
		t.Errorf("no page player: %s %s, playing %v", st.View.Key, st.View.Mode, st.View.Playing != nil)
	}
}

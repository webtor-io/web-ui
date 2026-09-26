package resource

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Every state of docs/transfer_status.html can be looked at on a dev
// server through the debugStatus preview (CLAUDE.md "Status badge"). The
// query next to each key is the one to open; the test proves it lands on
// that key, so a change to the state precedence cannot quietly make a state
// unreachable from the preview. stream_ok, stream_stall and stream_over are
// the page's refinement of "tier" by its own player (lib/transferStatus.js
// present) and are reached by playing the video on a tier preview
// (stream_over: a file the stream job marked data-status-over-cap).
var debugStateQueries = map[string]string{
	"active":         "debug_status=caching&progress=43&seeders=14&rate=4980736&user_rate=1572864&debug_pieces=stream",
	"tier_dl":        "debug_status=caching&progress=61&seeders=31&rate=4980736&plan_limited=1&debug_pieces=stream&bitrate=8",
	"stream_ok":      "debug_status=caching&progress=61&seeders=31&rate=4980736&plan_limited=1&debug_pieces=stream&bitrate=8",
	"stream_stall":   "debug_status=caching&progress=61&seeders=31&rate=4980736&plan_limited=1&debug_pieces=stream&bitrate=8",
	"stream_over":    "debug_status=caching&progress=61&seeders=31&rate=4980736&plan_limited=1&debug_pieces=stream&bitrate=8",
	"swarm":          "debug_status=caching&progress=8&seeders=2&rate=157286&user_rate=157286&debug_pieces=sparse",
	"stalled":        "debug_status=caching&progress=43&seeders=14&rate=4980736&viewer_stalled=1&debug_pieces=stream",
	"missing":        "debug_status=caching&progress=43&peers=12&seeders=0&availability=0.73&debug_missing=holes&wanted_missing=5&reader_missing=3&viewer_stalled=1&debug_pieces=stream",
	"caching_only":   "debug_status=caching&progress=43&seeders=14&rate=4980736&viewer=zero&debug_pieces=stream",
	"cached_flow":    "debug_status=cached&user_rate=3145728",
	"cached_tier":    "debug_status=cached&plan_limited=1",
	"checking":       "debug_status=caching&progress=43&seeders=14&checking=1&debug_pieces=stream",
	"paused":         "debug_status=caching&progress=43&seeders=14&paused=1&viewer=zero&debug_pieces=stream",
	"noseed":         "debug_status=caching&progress=43&noseeders=1&viewer=zero&debug_pieces=stream",
	"missing_idle":   "debug_status=caching&progress=43&peers=12&seeders=0&availability=0.73&debug_missing=holes&wanted_missing=5&viewer=zero&debug_pieces=stream",
	"idle_torrent":   "debug_status=idle&seeders=14&viewer=zero",
	"cached":         "debug_status=cached&viewer=zero",
	"status_unknown": "debug_status=unknown&viewer=zero",
	"caching_idle":   "debug_status=caching&progress=43&seeders=14&viewer=zero&debug_pieces=stream",
	"vaulting":       "debug_status=vaulting&progress=64&seeders=9&rate=2883584&user_rate=1572864&debug_pieces=stream",
	"vaulting_only":  "debug_status=vaulting&progress=64&seeders=9&rate=2883584&viewer=zero&debug_pieces=stream",
	"vaulted":        "debug_status=vaulted&user_rate=3145728",
	"vaulted_tier":   "debug_status=vaulted&plan_limited=1",
	"vaulted_idle":   "debug_status=vaulted&viewer=zero",
	"vault_waiting":  "debug_status=vault_waiting&seeders=0&viewer=zero&debug_pieces=sparse",
	"vault_missing":  "debug_status=vaulting&progress=58&peers=12&seeders=0&availability=0.73&debug_missing=holes&wanted_missing=5&viewer=zero&debug_pieces=half",
	"vault_failed":   "debug_status=vault_failed&progress=37&seeders=3&viewer=zero&debug_pieces=half",
	"vaulting_idle":  "debug_status=vaulting&progress=64&seeders=9&viewer=zero&debug_pieces=stream",
}

// debugHatched are the states whose preview hatches the pieces nobody has.
var debugHatched = map[string]bool{"missing": true, "missing_idle": true, "vault_missing": true}

// viewKeyOf is the server's key for a design key: the server cannot tell a
// stream from a download and says "tier" for all three.
func viewKeyOf(design string) string {
	switch design {
	case "tier_dl", "stream_ok", "stream_stall", "stream_over":
		return "tier"
	}
	return design
}

func TestDebugStatus_EveryDesignStateIsReachable(t *testing.T) {
	doc, err := os.ReadFile("../../docs/transfer_status.html")
	if err != nil {
		t.Fatal(err)
	}
	keys := regexp.MustCompile(`class="ts-key">([a-z_]+)<`).FindAllSubmatch(doc, -1)
	if len(keys) < 27 {
		t.Fatalf("%d design keys in docs/transfer_status.html", len(keys))
	}
	h := &Handler{} // no catalog: the preview's sample offer
	srv := statusServer(t, h, "free", "5M")
	for _, k := range keys {
		design := string(k[1])
		t.Run(design, func(t *testing.T) {
			q, ok := debugStateQueries[design]
			if !ok {
				t.Fatalf("no debugStatus query for the design key %q", design)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			msgs, _, _ := sseStream(ctx, t, srv.URL+"/"+ssHash+"/status?_csrf=tok&session=1&"+q)
			m := until(t, msgs, 3*time.Second, "the preview", func(m map[string]any) bool { return m["view"] != nil })
			if got := get(m, "view", "key"); got != viewKeyOf(design) {
				t.Errorf("?%s: view key %v, want %s", q, got, viewKeyOf(design))
			}
			if hatched := m["missing"] != nil; hatched != debugHatched[design] {
				t.Errorf("?%s: hatched %v", q, hatched)
			}
			// The bar where the design draws one: every preview that paints
			// pieces is a state with a bar (the view says whether they show;
			// Vault's wait for seeders draws the cache's, as approved).
			if bar, want := get(m, "view", "bar", "mode"), strings.Contains(q, "debug_pieces="); (bar == "pieces") != want {
				t.Errorf("?%s: bar %v", q, bar)
			}
		})
	}
}

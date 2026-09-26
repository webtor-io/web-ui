package resource

import (
	"bytes"
	"context"
	"encoding/json"
	"html/template"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/statusview"
)

// The Vault dashboard's rows (views/vault/index.html, app/vault/progress.js)
// draw the resource page's own badge: their stream -- this endpoint without
// session=1 -- carries View.Badge, built from the same status the same way
// with no viewer (the dashboard has none to draw), and nothing else of the
// view. Its older contract stays: state, progress, label, the end at
// vaulted.

// dashState is one status the dashboard's stream can present, as the loop
// hands it to present.
type dashState struct {
	design string
	key    string
	st     func() *TorrentStatus
}

func dashStates() []dashState {
	moving := func(state string, pct float64, seeders int, mbps float64) func() *TorrentStatus {
		return func() *TorrentStatus {
			return &TorrentStatus{State: state, Progress: pct, Seeders: seeders, Rate: txMbps(mbps), swarmKnown: true}
		}
	}
	still := func(st TorrentStatus) func() *TorrentStatus {
		return func() *TorrentStatus {
			s := st
			s.swarmStill = true
			return &s
		}
	}
	// holes: 12 peers, no seeder, the seeder knows 73% of the file is
	// between them and us, five wanted pieces nobody has.
	holes := func(state string, pct float64) TorrentStatus {
		return TorrentStatus{State: state, Progress: pct, Peers: 12, swarmKnown: true,
			availKnown: true, availability: 0.73, holes: true, wantedMissing: 5}
	}
	return []dashState{
		{"vaulting_only", statusview.KeyVaultingOnly, moving("vaulting", 64, 9, 22)},
		{"vaulting_idle", statusview.KeyVaultingIdle, still(TorrentStatus{State: "vaulting", Progress: 64, Seeders: 9, swarmKnown: true})},
		{"vault_missing", statusview.KeyVaultMissing, still(holes("vaulting", 58))},
		{"vault_waiting", statusview.KeyVaultWait, still(TorrentStatus{State: "vault_waiting", swarmKnown: true})},
		{"vault_failed", statusview.KeyVaultFailed, still(TorrentStatus{State: "vault_failed", Progress: 37, Seeders: 3, swarmKnown: true})},
		{"vaulted_idle", statusview.KeyVaultedIdle, still(TorrentStatus{State: "vaulted"})},
		{"caching_only", statusview.KeyCachingOnly, moving("caching", 43, 14, 38)},
		{"checking", statusview.KeyChecking, still(TorrentStatus{State: "caching", Progress: 43, Seeders: 14, swarmKnown: true, Checking: true})},
		{"paused", statusview.KeyPaused, still(TorrentStatus{State: "caching", Progress: 43, Seeders: 14, swarmKnown: true, Paused: true})},
		{"noseed", statusview.KeyNoSeed, still(TorrentStatus{State: "caching", Progress: 43, swarmKnown: true, NoSeeders: true})},
		{"missing_idle", statusview.KeyMissingIdle, still(holes("caching", 43))},
		{"cached", statusview.KeyCached, still(TorrentStatus{State: "cached", Progress: 100})},
		{"idle_torrent", statusview.KeyIdleTorrent, still(TorrentStatus{State: "idle", Seeders: 14, swarmKnown: true})},
		{"status_unknown", statusview.KeyUnknown, still(TorrentStatus{State: "unknown"})},
	}
}

func ruLoc() *i18n.Service { return i18n.New(os.DirFS("../../locales")) }

var dashT0 = time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

// Every state: the dashboard's message carries the badge the page's view has
// for the same status with no viewer, and no view.
func TestPresent_DashboardCarriesTheBadge(t *testing.T) {
	loc := ruLoc().Localizer("ru")
	for _, c := range dashStates() {
		t.Run(c.design, func(t *testing.T) {
			dash := &viewEnv{lang: "ru", loc: loc, tier: "free", offers: txOffers{}}
			page := &viewEnv{lang: "ru", loc: loc, tier: "free", offers: txOffers{}, withView: true}
			d, p := c.st(), c.st()
			dash.present(d, statusview.Viewer{}, statusview.Viewer{}, 0, 0, dashT0)
			page.present(p, statusview.Viewer{}, statusview.Viewer{}, 0, 0, dashT0)
			if p.View.Key != c.key {
				t.Fatalf("the page's key %s, want %s", p.View.Key, c.key)
			}
			if d.View != nil {
				t.Errorf("the dashboard got the whole view: %+v", d.View)
			}
			if p.Badge != nil {
				t.Errorf("the page got the badge twice, alone and in its view: %+v", p.Badge)
			}
			if d.Badge == nil || *d.Badge != p.View.Badge {
				t.Errorf("badge %+v, the page's %+v", d.Badge, p.View.Badge)
			}
			if d.Label == "" || d.State != p.State || d.Progress != p.Progress {
				t.Errorf("the old contract: state %q progress %v label %q", d.State, d.Progress, d.Label)
			}
		})
	}
}

// A reading is never the dashboard's: it draws no viewer, so a viewer's
// bytes do not turn "caching paused" into "caching" there (the page, which
// draws the viewer, says caching: the bytes come from the cache).
func TestPresent_DashboardBadgeHasNoViewer(t *testing.T) {
	loc := ruLoc().Localizer("ru")
	paused := func() *TorrentStatus {
		return &TorrentStatus{State: "caching", Progress: 43, Seeders: 14, swarmKnown: true, Paused: true, swarmStill: true}
	}
	reading := statusview.Viewer{Known: true, Present: true, Mbps: 12, CapMbps: 5}
	d, p := paused(), paused()
	(&viewEnv{lang: "ru", loc: loc, tier: "free"}).present(d, reading, statusview.Viewer{}, 0, 0, dashT0)
	(&viewEnv{lang: "ru", loc: loc, tier: "free", withView: true}).present(p, reading, statusview.Viewer{}, 0, 0, dashT0)
	if p.View.Key != statusview.KeyActive {
		t.Fatalf("the page with the reading: %s", p.View.Key)
	}
	if d.Badge == nil || d.Badge.Icon != "pause" || d.Badge.Tone != "warn" || !strings.HasPrefix(d.Badge.Label, "Кэширование на паузе") {
		t.Errorf("the dashboard with the same reading: %+v", d.Badge)
	}
}

// The dashboard's badge is held through the gaps between the swarm's pieces
// the way the page's view is (statusview.Hold, one per stream): a Vault
// transfer from peers without the whole file does not blink "waiting for
// missing pieces" between two of its pieces.
func TestPresent_DashboardBadgeHoldsThroughAGap(t *testing.T) {
	loc := ruLoc().Localizer("ru")
	env := &viewEnv{lang: "ru", loc: loc, tier: "free"}
	moving := &TorrentStatus{State: "vaulting", Progress: 58, Peers: 12, Rate: txMbps(22), swarmKnown: true,
		availKnown: true, availability: 0.73, holes: true, wantedMissing: 5}
	still := func() *TorrentStatus {
		s := *moving
		s.swarmStill = true
		return &s
	}
	env.present(moving, statusview.Viewer{}, statusview.Viewer{}, 0, 0, dashT0)
	gap := still()
	env.present(gap, statusview.Viewer{}, statusview.Viewer{}, 0, 0, dashT0.Add(2*time.Second))
	if gap.Badge == nil || gap.Badge.Icon != "up" || !strings.HasPrefix(gap.Badge.Label, "Сохраняется 58%") {
		t.Errorf("in a gap between pieces: %+v", gap.Badge)
	}
	over := still()
	env.present(over, statusview.Viewer{}, statusview.Viewer{}, 0, 0, dashT0.Add(statusview.HoldFor+time.Second))
	if over.Badge == nil || over.Badge.Icon != "clock" || !strings.HasPrefix(over.Badge.Label, "Ждём недостающие куски") {
		t.Errorf("after the hold: %+v", over.Badge)
	}
}

// End to end: the dashboard's stream of a cached torrent -- the real handler
// and loop against the fake rest-api -- carries the badge, and no view.
func TestStatusStream_DashboardCarriesTheBadge(t *testing.T) {
	node := newFakeNode(t, atCapEvents(3))
	h := &Handler{api: testAPI(t, node.srv), offers: liveOffers()}
	srv := statusServer(t, h, "free", "5M")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	msgs, _, _ := sseStream(ctx, t, srv.URL+"/"+ssHash+"/status?_csrf=tok")
	m := until(t, msgs, 5*time.Second, "the first status", func(m map[string]any) bool { return m["state"] == "cached" })
	if m["view"] != nil || m["label"] != "Cached" {
		t.Errorf("the old contract: %v", m)
	}
	want := map[string]any{"tone": "ok", "icon": "check", "label": "Cached"}
	if !reflect.DeepEqual(m["badge"], want) {
		t.Errorf("badge %v, want %v", m["badge"], want)
	}
}

// Through the debug preview, which takes the same presentation: every state
// the seeder's availability leads to has, on the dashboard's stream, exactly
// the badge the page's stream has in its view for the same status with no
// viewer.
func TestStatusStream_DashboardBadgeIsThePagesBadge(t *testing.T) {
	h := &Handler{}
	srv := statusServer(t, h, "free", "5M")
	const avail = "&peers=12&seeders=0&availability=0.73&debug_missing=holes&wanted_missing=5"
	for _, c := range []struct {
		query, key, label, extra string
	}{
		{"debug_status=vaulting&progress=58" + avail, statusview.KeyVaultMissing, "Waiting for missing pieces · 58%", ""},
		{"debug_status=caching&progress=43" + avail, statusview.KeyMissingIdle, "Needed pieces missing · 43%", "(12 peers, 0 seeders)"},
		{"debug_status=vaulting&progress=64&seeders=9", statusview.KeyVaultingIdle, "Vaulting 64%", "(9 seeders)"},
		{"debug_status=vault_waiting", statusview.KeyVaultWait, "Waiting for seeders", ""},
		{"debug_status=vault_failed&progress=37&seeders=3", statusview.KeyVaultFailed, "Transfer failed, retrying 37%", "(3 seeders)"},
		{"debug_status=caching&progress=43&seeders=14&paused=1", statusview.KeyPaused, "Caching paused 43%", "(14 seeders)"},
		{"debug_status=vaulted", statusview.KeyVaultedIdle, "Vaulted", ""},
	} {
		t.Run(c.key, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			first := func(q string) map[string]any {
				msgs, _, _ := sseStream(ctx, t, srv.URL+"/"+ssHash+"/status?_csrf=tok&"+q)
				return until(t, msgs, 3*time.Second, "the preview", func(map[string]any) bool { return true })
			}
			dash := first(c.query)
			page := first("session=1&" + c.query)
			if get(page, "view", "key") != c.key {
				t.Fatalf("the page's key %v, want %s", get(page, "view", "key"), c.key)
			}
			if dash["view"] != nil {
				t.Errorf("the dashboard got the view")
			}
			if page["badge"] != nil {
				t.Errorf("the page's stream carries the badge alone too: %v", page["badge"])
			}
			if !reflect.DeepEqual(dash["badge"], get(page, "view", "badge")) {
				t.Errorf("dashboard %v, page %v", dash["badge"], get(page, "view", "badge"))
			}
			if get(dash, "badge", "label") != c.label || (get(dash, "badge", "extra") != nil) != (c.extra != "") ||
				(c.extra != "" && get(dash, "badge", "extra") != c.extra) {
				t.Errorf("badge %v, want %q %q", dash["badge"], c.label, c.extra)
			}
		})
	}
}

// badgeTemplates parses the badge partial alone: the one element every page
// draws a status pill with.
func badgeTemplates(t *testing.T) *template.Template {
	t.Helper()
	tpl, err := template.New("badge.html").ParseFiles("../../templates/partials/status/badge.html")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return tpl
}

// The Vault page's own pills (a pledge "Saved", "Expiring", the row's badge
// before its first message) are the same element in one of its tones; the
// helper the template makes them with places them without an id -- a row
// each, an id would repeat.
func TestStatusBadgeHelper(t *testing.T) {
	got := NewHelper("s").StatusBadge("vault", "vault", "Сохранён")
	want := statusview.BadgeEl{Badge: statusview.Badge{Tone: "vault", Icon: "vault", Label: "Сохранён"}}
	if got != want {
		t.Errorf("%+v, want %+v", got, want)
	}
	html := txRender(t, badgeTemplates(t), "status/badge", got)
	if strings.Contains(html, " id=") || !strings.Contains(html, `data-tx-badge tabindex="-1" data-tone="vault" data-icon="vault"`) ||
		!strings.Contains(html, `<use href="#tx-b-vault">`) || !strings.Contains(html, `<span class="badge-text" title="Сохранён"><span data-tx-blabel>Сохранён</span>`) {
		t.Errorf("rendered:\n%s", html)
	}
	// The resource page's badge describes its chain.
	html = txRender(t, badgeTemplates(t), "status/badge", statusview.Badge{Tone: "cyan", Icon: "dots", Label: "x"}.El("tx-bdesc"))
	if !strings.Contains(html, `<span class="badge-text" id="tx-bdesc" title="x"><span data-tx-blabel>x</span>`) {
		t.Errorf("described:\n%s", html)
	}
}

// The words never wrap, and a phone's Vault column cuts most states ("Ждём
// сидо…"): the words carry their whole text -- label and swarm, as they
// read -- as a title, the one way left to read a cut badge with a mouse.
func TestStatusBadgeTitleIsItsWholeText(t *testing.T) {
	tpl := badgeTemplates(t)
	for _, c := range []struct {
		b     statusview.Badge
		title string
	}{
		{statusview.Badge{Tone: "vault", Icon: "up", Pulse: true, Label: "Сохраняется 64%", Extra: "(9 сидов)"}, "Сохраняется 64% (9 сидов)"},
		{statusview.Badge{Tone: "vault", Icon: "clock", Label: "Ждём сидов"}, "Ждём сидов"},
		{statusview.Badge{Tone: "warn", Icon: "warn", Label: `a "b" & <c>`}, `a &#34;b&#34; &amp; &lt;c&gt;`},
	} {
		html := txRender(t, tpl, "status/badge", c.b.El(""))
		if !strings.Contains(html, `<span class="badge-text" title="`+c.title+`">`) {
			t.Errorf("%+v:\n%s", c.b, html)
		}
	}
}

// The dev preview (debugStatus) is inert under GIN_MODE=release: /vault
// forwards debug_status and the rest from any page URL to every live row's
// stream (lib/statusDebug.js, no client-side gate), so the server's check
// is all that keeps a production stream from saying what its URL asks. The
// same request in test mode is the preview -- the query is not what fails.
func TestStatusStream_DebugPreviewIsInertInRelease(t *testing.T) {
	node := newFakeNode(t, atCapEvents(3))
	h := &Handler{api: testAPI(t, node.srv), offers: liveOffers()}
	srv := statusServer(t, h, "free", "5M")
	prev := gin.Mode()
	t.Cleanup(func() { gin.SetMode(prev) })
	first := func(q string) map[string]any {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		msgs, _, _ := sseStream(ctx, t, srv.URL+"/"+ssHash+"/status?_csrf=tok&"+q)
		return until(t, msgs, 5*time.Second, "the first status", func(map[string]any) bool { return true })
	}
	const q = "debug_status=vaulted"
	gin.SetMode(gin.TestMode)
	if m := first(q); m["state"] != "vaulted" {
		t.Fatalf("test mode: the preview not taken: %v", m)
	}
	gin.SetMode(gin.ReleaseMode)
	if m := first(q); m["state"] != "cached" || get(m, "badge", "label") != "Cached" {
		t.Errorf("release: %v, want the torrent's own status (cached)", m)
	}
}

// vaultState is one dashboard message and the badge a fresh server render
// draws for it: the fixture assets/src/js/lib/vaultProgress.test.js runs
// app/vault/progress.js against.
type vaultState struct {
	Design string         `json:"design"`
	Status *TorrentStatus `json:"status"`
	SSR    string         `json:"ssr"`
}

const vaultFixtureRegen = `UPDATE_FIXTURES=1 go test ` +
	`-ldflags '-X google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=ignore' ` +
	`./handlers/resource/ -run TestVaultStatusFixturesAreCurrent`

// The dashboard's messages for every state, generated through present, and
// the partial's render of each one's badge. Committed, because `npm test`
// must not need a Go toolchain; generated, because hand-written messages
// would keep passing after the stream changed shape.
func TestVaultStatusFixturesAreCurrent(t *testing.T) {
	loc := ruLoc().Localizer("ru")
	tpl := badgeTemplates(t)
	var states []vaultState
	for _, c := range dashStates() {
		st := c.st()
		(&viewEnv{lang: "ru", loc: loc, tier: "free"}).present(st, statusview.Viewer{}, statusview.Viewer{}, 0, 0, dashT0)
		states = append(states, vaultState{Design: c.design, Status: st, SSR: strings.TrimSpace(txRender(t, tpl, "status/badge", st.Badge.El("")))})
	}
	js, err := json.MarshalIndent(states, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	got := append(js, '\n')
	const path = "../../assets/src/js/lib/__fixtures__/vault-status-states.json"
	if os.Getenv("UPDATE_FIXTURES") != "" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v — generate it with:\n\n    %s", err, vaultFixtureRegen)
	}
	if !bytes.Equal(want, got) {
		t.Errorf("%s is stale. Regenerate it, then run `npm test`:\n\n    %s", path, vaultFixtureRegen)
	}
}

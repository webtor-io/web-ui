package resource

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"html/template"
	"os"
	"regexp"
	"strings"
	"testing"

	uuid "github.com/satori/go.uuid"
	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/offer"
	"github.com/webtor-io/web-ui/services/statusview"
	w "github.com/webtor-io/web-ui/services/web"
)

// The transfer status block as the server renders it (partials/resource/
// status.html) for every state of docs/transfer_status.html, and the
// fixtures assets/src/js/lib/transferStatus.test.js and
// statusView.test.js run against: the page's own markup, and each state's
// stream message next to the block the server renders for it -- so the JS
// test can prove that updating the page's block in place ends where a fresh
// render would, without ever adding or removing a node.

const txTorrentName = "Sintel.2010.1080p.WEB-DL"

// txOffers is the storefront of 2026-09-24 (as in services/statusview's
// tests): silver, 50 Mbps with a 7-day trial, is the promo plan; something
// faster than 100 Mbps is not on sale.
type txOffers struct{}

func (txOffers) Promo() *offer.Offer {
	return &offer.Offer{Tier: "silver", PeriodDays: 30, RateMbps: 50, TrialDays: 7, URL: "https://pay.example/silver"}
}
func (txOffers) FasterOnSale(r float64) bool { return r < 100 }

// txMbps is a swarm rate in bytes a second that reads v Mbps (the rate
// claim's megabit, 2^20 bits).
func txMbps(v float64) float64 { return v * (1 << 20) / 8 }

// txState is one row of the design: the status stream message (its `view`
// built by statusview.Build), what the page's player does, and the key the
// page must end up showing.
type txState struct {
	Design string `json:"design"`
	// Player is lib/playerActivity.js's verdict the JS test applies it with.
	Player string `json:"player"`
	// StallSub is the player's own data-status-stall-sub, when it has one.
	StallSub string `json:"stallSub,omitempty"`
	// OverCap is the player's data-status-over-cap: the stream job knows
	// the file needs more than the cap.
	OverCap bool           `json:"overCap,omitempty"`
	Status  *TorrentStatus `json:"status"`
	// SSR is partials/resource/status.html rendered for the same view: what
	// a fresh page would show. Only for Player "none" -- the server cannot
	// know the player and draws the download variant.
	SSR string `json:"ssr,omitempty"`
}

func txStates(t *testing.T) []txState {
	t.Helper()
	loc := i18n.New(os.DirFS("../../locales")).Localizer("ru")
	fill, active, total := debugPieces("stream")
	const gb12 = 1288490189
	type row struct {
		design, player, stallSub string
		overCap                  bool
		torrent                  statusview.Torrent
		viewer                   statusview.Viewer
		// last: the viewer's last reading on the chain while they read
		// gone (statusview.Input.LastViewer).
		last    statusview.Viewer
		bitrate float64
	}
	caching := func(pct float64, seeders int, rate float64) statusview.Torrent {
		return statusview.Torrent{State: "caching", Progress: pct, Seeders: seeders, SwarmKnown: true, RateBps: txMbps(rate), Pieces: true}
	}
	// holes: 12 peers, no seeder, 73% of the torrent between them and us.
	holes := func(state string, pct float64, reader int) statusview.Torrent {
		return statusview.Torrent{State: state, Progress: pct, Peers: 12, SwarmKnown: true, Pieces: true,
			AvailabilityKnown: true, Availability: 0.73, Missing: true, WantedMissing: 5, ReaderMissing: reader}
	}
	// On the chain (a request open): flowing, at the cap, waiting; zero --
	// numbers arrive, no request of theirs is open.
	flowing := func(v float64) statusview.Viewer {
		return statusview.Viewer{Known: true, Present: true, Mbps: v, CapMbps: 5}
	}
	zero := statusview.Viewer{Known: true, CapMbps: 5}
	// At the cap long enough for the box (the design's rows); capFact its
	// first seconds: the pink link, nothing sold yet.
	atCap := statusview.Viewer{Known: true, Present: true, Mbps: 5, Limited: true, PlanBox: true, CapMbps: 5}
	capFact := statusview.Viewer{Known: true, Present: true, Mbps: 5, Limited: true, CapMbps: 5}
	stalled := statusview.Viewer{Known: true, Present: true, Stalled: true, CapMbps: 5}
	vaulting := func(rate float64) statusview.Torrent {
		return statusview.Torrent{State: "vaulting", Progress: 64, Seeders: 9, SwarmKnown: true, RateBps: txMbps(rate), Pieces: true}
	}
	rows := []row{
		{design: "active", torrent: caching(43.4, 14, 38), viewer: flowing(12)},
		{design: "tier_dl", torrent: caching(61, 31, 38), viewer: atCap},
		{design: "stream_ok", player: "playing", torrent: caching(61, 31, 38), viewer: atCap},
		{design: "stream_stall", player: "buffering", torrent: caching(61, 31, 38), viewer: atCap,
			stallSub: "Без подписки — до 5 Мбит/с, а файлу нужно 8 Мбит/с"},
		// Playing smoothly from its buffer, but the stream job knows the
		// file needs more than the cap: the stream box once it is due, not
		// at the first stall (owner, 2026-09-25/26).
		{design: "stream_over", player: "playing", overCap: true, torrent: caching(61, 31, 38), viewer: atCap,
			stallSub: "Без подписки — до 5 Мбит/с, а файлу нужно 8 Мбит/с"},
		{design: "swarm", torrent: caching(8, 2, 1.2), viewer: flowing(1.2)},
		{design: "stalled", torrent: caching(43, 14, 38), viewer: stalled},
		{design: "missing", torrent: holes("caching", 43, 3), viewer: stalled},
		{design: "caching_only", torrent: caching(43, 14, 38), viewer: zero},
		{design: "cached_flow", torrent: statusview.Torrent{State: "cached", Progress: 100}, viewer: flowing(24)},
		{design: "cached_tier", torrent: statusview.Torrent{State: "cached", Progress: 100}, viewer: atCap},
		{design: "checking", torrent: statusview.Torrent{State: "caching", Progress: 43, Seeders: 14, SwarmKnown: true, Checking: true, Pieces: true}, viewer: zero},
		{design: "paused", torrent: statusview.Torrent{State: "caching", Progress: 43, Seeders: 14, SwarmKnown: true, Paused: true, Pieces: true}, viewer: zero},
		{design: "noseed", torrent: statusview.Torrent{State: "caching", Progress: 43, SwarmKnown: true, NoSeeders: true, Pieces: true}, viewer: zero},
		{design: "missing_idle", torrent: holes("caching", 43, 0), viewer: zero},
		{design: "idle_torrent", torrent: statusview.Torrent{State: "idle", Seeders: 14, SwarmKnown: true}, viewer: zero},
		{design: "cached", torrent: statusview.Torrent{State: "cached", Progress: 100}, viewer: zero},
		{design: "status_unknown", torrent: statusview.Torrent{State: "unknown"}, viewer: zero},
		{design: "caching_idle", torrent: caching(43, 14, 0), viewer: zero},
		{design: "vaulting", torrent: vaulting(22), viewer: flowing(12)},
		{design: "vaulting_only", torrent: vaulting(22), viewer: zero},
		{design: "vaulted", torrent: statusview.Torrent{State: "vaulted"}, viewer: flowing(24)},
		{design: "vaulted_tier", torrent: statusview.Torrent{State: "vaulted"}, viewer: atCap},
		{design: "vaulted_idle", torrent: statusview.Torrent{State: "vaulted"}, viewer: zero},
		// The cache's pieces under Vault's wait, in its purple (approved).
		{design: "vault_waiting", torrent: statusview.Torrent{State: "vault_waiting", SwarmKnown: true, Pieces: true}, viewer: zero},
		{design: "vault_missing", torrent: holes("vaulting", 58, 0), viewer: zero},
		{design: "vault_failed", torrent: statusview.Torrent{State: "vault_failed", Progress: 37, Seeders: 3, SwarmKnown: true, Pieces: true}, viewer: zero},
		{design: "vaulting_idle", torrent: vaulting(0), viewer: zero},
		// Not a row of the design: the cap's first seconds, before
		// the box is due (statusview.PlanBoxAfter) -- the pink link and the
		// key that says the cap, and no box or line under the bar yet.
		{design: "tier_fact", torrent: caching(61, 31, 38), viewer: capFact},
		// Not a row of the design either: between two HLS segments of cached
		// content -- thp counts no request of the viewer's open past the
		// debounce, so the view is the badge ("cached"), and it carries the
		// view with them on the chain at their last reading (View.Playing:
		// cached_flow) that the page draws while its own player plays.
		{design: "hls_gap", torrent: statusview.Torrent{State: "cached", Progress: 100}, viewer: zero, last: flowing(24)},
	}
	out := make([]txState, 0, len(rows))
	for _, r := range rows {
		v := statusview.Build(statusview.Input{
			Lang: "ru", Loc: loc, Torrent: r.torrent, Viewer: r.viewer, LastViewer: r.last, ClaimCapMbps: 5,
			Offers: txOffers{}, SizeBytes: gb12, BitrateMbps: r.bitrate,
		})
		st := &TorrentStatus{State: r.torrent.State, Progress: r.torrent.Progress, Seeders: r.torrent.Seeders, Peers: r.torrent.Peers, View: v}
		if r.torrent.Pieces {
			st.Pieces = base64.StdEncoding.EncodeToString(fill)
			st.Active = base64.StdEncoding.EncodeToString(active)
			st.PiecesTotal = total
			st.PiecesLabel = "40 из 1024 кусков на сервере"
			if r.torrent.Missing {
				st.Missing = base64.StdEncoding.EncodeToString(debugHoles("holes"))
			}
		}
		player := r.player
		if player == "" {
			player = "none"
		}
		out = append(out, txState{Design: r.design, Player: player, StallSub: r.stallSub, OverCap: r.overCap, Status: st})
	}
	return out
}

// txTemplates parses the resource view with the status partial; the view's
// other templates are not executed here.
func txTemplates(t *testing.T) *template.Template {
	t.Helper()
	funcs := intentFuncs(t)
	// A constant token: the fixture must not change every second.
	funcs["statusToken"] = func(string) string { return "status-token" }
	tpl, err := template.New("get.html").Funcs(funcs).ParseFiles(
		"../../templates/views/resource/get.html",
		"../../templates/partials/resource/status.html",
		"../../templates/partials/status/badge.html",
	)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return tpl
}

// txPage is a resource page as the handler leaves it before the stream: the
// Vault database's word (idle) read as "checking", the viewer not drawn.
func txPage() *w.Context {
	st := &TorrentStatus{State: "idle"}
	env := &viewEnv{lang: "ru", loc: i18n.New(os.DirFS("../../locales")).Localizer("ru"), tier: "free", offers: txOffers{}}
	gd := &GetData{
		Args: &GetArgs{ID: intentHash, Page: 1, PageSize: pageSize},
		Resource: &ExtendedResource{ResourceResponse: &ra.ResourceResponse{
			ID: intentHash, Name: txTorrentName, MagnetURI: "magnet:?xt=urn:btih:" + intentHash,
		}},
		Item:          &ra.ListItem{ID: "item-mkv", Name: "Sintel.mkv", PathStr: "/Sintel/Sintel.mkv", Type: ra.ListTypeFile},
		TorrentStatus: st,
		StatusView:    env.build(st, statusview.Viewer{}, statusview.Viewer{}, 0, 0, true, 0),
	}
	// Vault configured, an anonymous viewer: the hint's Vault link leads
	// to the login first, the way the page's own Vault button does.
	return &w.Context{Lang: "ru", Data: gd, User: &auth.User{}, CSRF: "csrf-token", Path: "/" + intentHash, Vault: true}
}

func txRender(t *testing.T, tpl *template.Template, name string, data any) string {
	t.Helper()
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, name, data); err != nil {
		t.Fatalf("render %s: %v", name, err)
	}
	return buf.String()
}

// txBlock is the .tx block of a render, without the icon sprite before it.
func txBlock(html string) string {
	if i := strings.Index(html, `<div class="tx"`); i >= 0 {
		return strings.TrimSpace(html[i:])
	}
	return ""
}

// The slots are fixed whatever the state -- the promise the client's
// in-place updates rest on -- and what the server says is what is drawn:
// the chain's participants, or the badge.
func TestTransferStatusSSR(t *testing.T) {
	tpl := txTemplates(t)
	ctx := txPage()
	count := func(s, sub string) int { return strings.Count(s, sub) }
	for _, st := range txStates(t) {
		t.Run(st.Design, func(t *testing.T) {
			v := st.Status.View
			html := txBlock(txRender(t, tpl, "resource/status", (&w.Helper{}).WithContext(ctx, v)))
			for sub, want := range map[string]int{
				"data-tx-node":   3,
				"data-tx-seg":    2,
				"data-tx-badge ": 1,
				"data-tx-row":    4,
				"data-tx-pbox":   2,
				"data-tx-cta ":   2,
				"data-tx-hint":   1,
				"data-tx-vault":  1,
				"data-tx-bar":    1,
				`class="tx-ln"`:  2,
				`class="tx-hd"`:  2,
				"data-tx-toggle": 1,
			} {
				if n := count(html, sub); n != want {
					t.Errorf("%d × %s, want %d", n, sub, want)
				}
			}
			if !strings.Contains(html, `data-mode="`+v.Mode+`"`) {
				t.Errorf("the block without its mode %q", v.Mode)
			}
			// The chain's accessible name is the popover's title, fixed: from
			// its content it would change every second under a screen
			// reader's focus.
			if !strings.Contains(html, `data-tx-toggle popovertarget="tx-details" aria-expanded="false" aria-label="`+v.Details.Title+`"`) {
				t.Errorf("the chain without its fixed name")
			}
			// Its description is the badge's words: the state as text,
			// which the badge cannot say while it is invisible. The words
			// carry their whole text as a title too, for a badge its row
			// cuts (partials/status/badge.html).
			whole := v.Badge.Label
			if v.Badge.Extra != "" {
				whole += " " + v.Badge.Extra
			}
			if !strings.Contains(html, `aria-describedby="tx-bdesc"`) || count(html, `id="tx-bdesc"`) != 1 ||
				!strings.Contains(html, `<span class="badge-text" id="tx-bdesc" title="`+template.HTMLEscapeString(whole)+`"><span data-tx-blabel>`+template.HTMLEscapeString(v.Badge.Label)+`</span>`) {
				t.Errorf("the chain is not described by the badge's words")
			}
			for i, n := range v.Nodes {
				if n.Show && !strings.Contains(html, `<b class="tx-nm">`+template.HTMLEscapeString(n.Name)+`</b>`) {
					t.Errorf("node %d: name %q not drawn", i, n.Name)
				}
			}
			for i, s := range v.Segs {
				if s.Speed != "" && !strings.Contains(html, `<span class="tx-spd">`+s.Speed+`</span>`) {
					t.Errorf("seg %d: speed %q not drawn", i, s.Speed)
				}
			}
			b := v.Badge
			if !strings.Contains(html, `data-tx-badge tabindex="-1" data-tone="`+b.Tone+`" data-icon="`+b.Icon+`"`) ||
				!strings.Contains(html, `<use href="#tx-b-`+b.Icon+`">`) ||
				!strings.Contains(html, `<span data-tx-blabel>`+template.HTMLEscapeString(b.Label)+`</span>`) ||
				!strings.Contains(html, `data-tx-bextra>`+template.HTMLEscapeString(b.Extra)+`</span>`) {
				t.Errorf("the badge %+v not drawn:\n%s", b, html)
			}
			// Hidden slots: one per node, segment and row that is not shown,
			// plus the box pair and the details' box when there is no plan,
			// the hint without one, and the Vault link where it is not
			// offered.
			hidden := 0
			for _, n := range v.Nodes {
				if !n.Show {
					hidden++
				}
			}
			for _, s := range v.Segs {
				if !s.Show {
					hidden++
				}
			}
			for _, r := range v.Details.Rows {
				if !r.Show {
					hidden++
				}
				if r.Tag == "" {
					hidden++
				}
			}
			box := v.Plan != nil && v.Plan.Download.Box != nil
			if !box {
				hidden += 3
			}
			hint := v.Hint
			if v.Plan != nil && v.Plan.Download.Hint != "" {
				hint = v.Plan.Download.Hint
			}
			if hint == "" {
				hidden++
			}
			if !v.Vault {
				hidden++
			}
			if n := count(html, " hidden"); n != hidden {
				t.Errorf("%d hidden slots, want %d", n, hidden)
			}
			if box && st.Player == "none" {
				b := v.Plan.Download.Box
				if !strings.Contains(html, `href="`+b.CTA.URL+`"`) || !strings.Contains(html, `data-umami-event-state="`+st.Design+`"`) {
					t.Errorf("the download box without its link or state:\n%s", html)
				}
			}
		})
	}
}

// The Vault link is the page's Vault button in words: the pledge form for a
// signed-in viewer, the login first for anyone else -- and nothing at all
// where Vault is not configured (capability, not deployment: CLAUDE.md).
func TestTransferStatusVaultLink(t *testing.T) {
	tpl := txTemplates(t)
	var missing *statusview.View
	for _, st := range txStates(t) {
		if st.Design == "missing_idle" {
			missing = st.Status.View
		}
	}
	anon := txPage()
	html := txBlock(txRender(t, tpl, "resource/status", (&w.Helper{}).WithContext(anon, missing)))
	if !strings.Contains(html, `href="/ru/login?from=vault&return-url=%2fru%2f`+intentHash+`" data-async-target="main" data-umami-event="vault-clicked-anonymous" data-umami-event-location="status">`) {
		t.Errorf("anonymous: the login first:\n%s", html)
	}
	if !strings.Contains(html, "Сохранить в Vault — догрузит сам") {
		t.Error("the link's words")
	}
	signedIn := txPage()
	signedIn.User = &auth.User{ID: uuid.FromStringOrNil("7c1b5f7e-4f4e-4b43-9d4e-1f9a1b2c3d4e")}
	html = txBlock(txRender(t, tpl, "resource/status", (&w.Helper{}).WithContext(signedIn, missing)))
	if !strings.Contains(html, `href="/ru/`+intentHash+`?pledge-form=true" data-async-target="#pledge-modal" data-async-push-state="false" data-umami-event="vault-clicked" data-umami-event-location="status">`) {
		t.Errorf("signed in: the pledge form:\n%s", html)
	}
	noVault := txPage()
	noVault.Vault = false
	if html := txBlock(txRender(t, tpl, "resource/status", (&w.Helper{}).WithContext(noVault, missing))); strings.Contains(html, "data-tx-vault") || strings.Contains(html, "Vault — ") {
		t.Errorf("no Vault configured, yet a link:\n%s", html)
	}
}

// The torrent's name is nowhere in the status or the sticky bar (owner,
// 2026-09-24) -- it used to sit next to the badge in both.
func TestTransferStatusHasNoTorrentName(t *testing.T) {
	tpl := txTemplates(t)
	ctx := txPage()
	for _, name := range []string{"resource/status_container", "resource/status_sticky"} {
		if html := txRender(t, tpl, name, ctx); strings.Contains(html, txTorrentName) {
			t.Errorf("%s names the torrent:\n%s", name, html)
		}
	}
}

// Every row of the design has a state here; a row added there fails.
func TestTransferStatusCoversTheDesign(t *testing.T) {
	b, err := os.ReadFile("../../docs/transfer_status.html")
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, st := range txStates(t) {
		have[st.Design] = true
	}
	keys := regexp.MustCompile(`class="ts-key">([a-z_]+)<`).FindAllStringSubmatch(string(b), -1)
	if len(keys) < 27 {
		t.Fatalf("%d state keys in the design, expected all 27", len(keys))
	}
	for _, m := range keys {
		if !have[m[1]] {
			t.Errorf("docs/transfer_status.html has state %q, not rendered here", m[1])
		}
	}
}

const txFixtureRegen = `UPDATE_FIXTURES=1 go test ` +
	`-ldflags '-X google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=ignore' ` +
	`./handlers/resource/ -run TestTransferStatusFixturesAreCurrent`

// The markup and the messages the JS tests run against. Committed, because
// `npm test` must not need a Go toolchain; generated, because a hand-written
// copy of the block would keep passing after the partial changed.
func TestTransferStatusFixturesAreCurrent(t *testing.T) {
	tpl := txTemplates(t)
	ctx := txPage()
	page := "<!-- Generated by handlers/resource TestTransferStatusFixturesAreCurrent — do not edit.\n     " + txFixtureRegen + " -->\n" +
		txRender(t, tpl, "resource/status_container", ctx) + "\n" +
		txRender(t, tpl, "resource/status_sticky", ctx) + "\n"
	states := txStates(t)
	for i := range states {
		if states[i].Player == "none" {
			states[i].SSR = txBlock(txRender(t, tpl, "resource/status", (&w.Helper{}).WithContext(ctx, states[i].Status.View)))
		}
	}
	js, err := json.MarshalIndent(states, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"../../assets/src/js/lib/__fixtures__/transfer-status-page.html":   []byte(page),
		"../../assets/src/js/lib/__fixtures__/transfer-status-states.json": append(js, '\n'),
	}
	for path, got := range files {
		if os.Getenv("UPDATE_FIXTURES") != "" {
			if err := os.WriteFile(path, got, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v — generate it with:\n\n    %s", err, txFixtureRegen)
		}
		if !bytes.Equal(want, got) {
			t.Errorf("%s is stale. Regenerate it, then run `npm test`:\n\n    %s", path, txFixtureRegen)
		}
	}
}

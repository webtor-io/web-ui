package vault

import (
	"bytes"
	"html"
	"html/template"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	wr "github.com/webtor-io/web-ui/handlers/resource"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/statusview"
	"github.com/webtor-io/web-ui/services/vault"
	"github.com/webtor-io/web-ui/services/web"
)

const testSecret = "vault-render-test-secret"

const (
	loadingHash = "4897aacef168307328e121694dcdb8407e6d62cc"
	vaultedHash = "08ada5a7a6183aae1e09d831df6748d566095a10"
)

// parseVault parses vault/index.html and the badge partial it draws every
// status pill with, with the functions the page uses; statusToken and
// statusBadge are the resource helper's, signing with testSecret.
func parseVault(t *testing.T) *template.Template {
	t.Helper()
	return parseVaultWith(t, wr.NewHelper(testSecret).StatusToken)
}

func parseVaultWith(t *testing.T, statusToken func(string) string) *template.Template {
	t.Helper()
	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { locales.Close() })
	ih := i18n.NewHelper(i18n.New(locales.FS()))
	wh := &web.Helper{}
	tpl, err := template.New("index.html").Funcs(template.FuncMap{
		"t":            ih.T,
		"tp":           ih.Tp,
		"langPath":     i18n.LangPath,
		"duration":     wh.Duration,
		"derefFloat64": wh.DerefFloat64,
		"asset": func(name string) template.HTML {
			return template.HTML(`<script src="/assets/` + name + `"></script>`)
		},
		"statusToken": statusToken,
		"statusBadge": wr.NewHelper(testSecret).StatusBadge,
	}).ParseFiles("../../templates/views/vault/index.html", "../../templates/partials/status/badge.html")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return tpl
}

type vaultView struct {
	Lang, CSRF string
	Data       *PledgeListData
}

func pledgeData() *PledgeListData {
	loading := &vaultModels.Resource{ResourceID: loadingHash, Name: "loading", Funded: true}
	vaulted := &vaultModels.Resource{ResourceID: vaultedHash, Name: "vaulted", Funded: true, Vaulted: true}
	enriched := []vault.EnrichedPledge{
		{Pledge: vaultModels.Pledge{ResourceID: loadingHash, Resource: loading, Funded: true, Amount: 1}},
		{Pledge: vaultModels.Pledge{ResourceID: vaultedHash, Resource: vaulted, Funded: true, Amount: 1}},
	}
	return &PledgeListData{
		Pledges: buildPledgeDisplay(enriched, time.Hour),
		Stats:   &vault.UserStats{},
	}
}

var progressRowRe = regexp.MustCompile(`<tr [^>]*data-vault-progress[^>]*>`)
var attrRe = func(name string) *regexp.Regexp {
	return regexp.MustCompile(name + `="([^"]*)"`)
}

// progressRows maps each live-progress row's resource id to the status token
// it carries ("" when the attribute is missing).
func progressRows(t *testing.T, out string) map[string]string {
	t.Helper()
	rows := map[string]string{}
	for _, tag := range progressRowRe.FindAllString(out, -1) {
		id := attrRe("data-resource-id").FindStringSubmatch(tag)
		if id == nil {
			t.Fatalf("progress row without a resource id: %s", tag)
		}
		tok := ""
		if m := attrRe("data-status-token").FindStringSubmatch(tag); m != nil {
			tok = m[1]
		}
		rows[id[1]] = tok
	}
	return rows
}

func checkProgressRows(t *testing.T, where, out string) {
	t.Helper()
	rows := progressRows(t, out)
	if len(rows) != 1 {
		t.Fatalf("%s: want exactly the loading pledge to show progress, got %v", where, rows)
	}
	tok, ok := rows[loadingHash]
	if !ok {
		t.Fatalf("%s: the loading pledge has no progress row: %v", where, rows)
	}
	// The status stream refuses to open without this token (resource
	// status.go); a row without one shows a spinner forever — /vault from
	// 2026-09-07 to 2026-09-24.
	if err := wr.CheckStatusToken(testSecret, tok, loadingHash, time.Now()); err != nil {
		t.Fatalf("%s: the progress row's status token is not accepted by the stream: %v (token %q)", where, err, tok)
	}
	if !strings.Contains(out, `src="/assets/vault/progress.js"`) {
		t.Fatalf("%s: vault/progress.js is not rendered with the rows", where)
	}
}

// Every pledge that shows live progress hands the status stream a token the
// stream accepts, on the full page and on the async reload progress.js makes
// when the tokens expire.
func TestVaultProgressRowsCarryStatusToken(t *testing.T) {
	tpl := parseVault(t)
	for _, lang := range []string{"en", "ru"} {
		view := vaultView{Lang: lang, CSRF: "csrf", Data: pledgeData()}

		var page bytes.Buffer
		if err := tpl.ExecuteTemplate(&page, "main", view); err != nil {
			t.Fatalf("%s: execute main: %v", lang, err)
		}
		checkProgressRows(t, lang+" page", page.String())

		// The reload sends the wrapper's data-async-layout as X-Layout and
		// renders it against the same view (services/template WithLayoutBody).
		m := regexp.MustCompile(`id="vault-pledges" data-async-layout="([^"]*)"`).FindStringSubmatch(page.String())
		if m == nil {
			t.Fatalf("%s: the pledges table has no async-layout wrapper to reload", lang)
		}
		layout := template.Must(parseVault(t).New("x-layout").Parse(html.UnescapeString(m[1])))
		var reload bytes.Buffer
		if err := layout.ExecuteTemplate(&reload, "x-layout", view); err != nil {
			t.Fatalf("%s: execute reload layout %q: %v", lang, m[1], err)
		}
		checkProgressRows(t, lang+" reload", reload.String())
	}
}

// badgeRe is one status badge element (partials/status/badge.html) with its
// tone, icon, pulse, glyph, the words' title and the words.
var badgeRe = regexp.MustCompile(`<span class="tx-badge badge badge-sm gap-1.5 px-3 py-0.5" data-tx-badge tabindex="-1" data-tone="([a-z]*)" data-icon="([a-z]*)"( data-pulse)?><svg class="tx-bi" aria-hidden="true"><use href="#tx-b-([a-z]*)"></use></svg><span class="tx-bdots loading loading-dots loading-xs" aria-hidden="true"></span><span class="badge-text" title="([^"]*)"><span data-tx-blabel>([^<]*)</span> <span class="tx-bx opacity-70" data-tx-bextra>([^<]*)</span></span></span>`)

// pendingBadge is statusview's badge for a torrent nothing has been asked
// about yet ("checking"), as the badge regexp reads it: what a live row
// shows before its stream's first message.
func pendingBadge(t *testing.T, lang string) []string {
	t.Helper()
	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { locales.Close() })
	v := statusview.Build(statusview.Input{Lang: lang, Loc: i18n.New(locales.FS()).Localizer(lang),
		Torrent: statusview.Torrent{State: "idle", Pending: true}})
	if v.Key != statusview.KeyChecking {
		t.Fatalf("a pending torrent is %s, not checking", v.Key)
	}
	b := v.Badge
	pulse := ""
	if b.Pulse {
		pulse = " data-pulse"
	}
	title := b.Label
	if b.Extra != "" {
		title += " " + b.Extra
	}
	return []string{b.Tone, b.Icon, pulse, b.Icon, title, b.Label, b.Extra}
}

// cellBadges are the badges in each row's status cell, keyed by the row's
// resource name.
func cellBadges(t *testing.T, out string) map[string][][]string {
	t.Helper()
	rows := map[string][][]string{}
	for _, tr := range regexp.MustCompile(`(?s)<tr [^>]*>.*?</tr>`).FindAllString(out, -1) {
		cells := regexp.MustCompile(`(?s)<td[^>]*>.*?</td>`).FindAllString(tr, -1)
		if len(cells) != 4 {
			continue
		}
		name := strings.TrimSpace(regexp.MustCompile(`(?s)<a [^>]*>(.*?)</a>`).FindStringSubmatch(cells[1])[1])
		rows[name] = badgeRe.FindAllStringSubmatch(cells[3], -1)
	}
	return rows
}

// Every status pill on the page is the transfer status's one badge element:
// each live row's before its first message (statusview's own "checking" --
// a real state, the size of every other badge, not an empty pill of dots;
// vault/progress.js fills it in place), a vaulted pledge's "Saved" in
// Vault's purple and layers -- exactly what a live row turns into on its
// last message -- and "Expiring" in its pink with a clock; the status
// guide's pills are the same. Every badge's words carry their whole text as
// a title (a column that cuts them leaves it readable). Its icons are on the
// page once, no badge carries an id (a row each: it would repeat), and no
// pill of the old markup is left.
func TestVaultRowsDrawTheStatusBadge(t *testing.T) {
	tpl := parseVault(t)
	for _, lang := range []string{"en", "ru"} {
		t.Run(lang, func(t *testing.T) {
			var page bytes.Buffer
			if err := tpl.ExecuteTemplate(&page, "main", vaultView{Lang: lang, CSRF: "csrf", Data: fixturePledges()}); err != nil {
				t.Fatal(err)
			}
			out := page.String()
			saved := map[string]string{"en": "Saved", "ru": "Сохранён"}[lang]
			expiring := map[string]string{"en": "Expiring", "ru": "Истекает"}[lang]
			want := map[string][]string{
				"loading":  pendingBadge(t, lang),
				"second":   pendingBadge(t, lang),
				"vaulted":  {"vault", "vault", "", "vault", saved, saved, ""},
				"expiring": {"pink", "clock", "", "clock", expiring, expiring, ""},
			}
			if w := want["loading"]; w[0] != "cyan" || w[1] != "dots" || w[5] == "" {
				t.Errorf("statusview's checking badge: %q", w)
			}
			got := cellBadges(t, out)
			for name, w := range want {
				if len(got[name]) != 1 {
					t.Errorf("%s: %d badges in the status cell, want 1", name, len(got[name]))
					continue
				}
				if b := got[name][0][1:]; !reflect.DeepEqual(b, w) {
					t.Errorf("%s: badge %q, want %q", name, b, w)
				}
			}
			// The status guide explains the same two pills, drawn the same.
			guide := out[strings.Index(out, "collapse-content"):]
			if n := len(badgeRe.FindAllString(guide, -1)); n != 2 {
				t.Errorf("%d badges in the status guide, want 2", n)
			}
			for _, name := range []string{"vaulted", "expiring"} {
				if len(got[name]) == 1 && !strings.Contains(guide, got[name][0][0]) {
					t.Errorf("the guide's pill for %s is not the row's", name)
				}
			}
			if n := strings.Count(out, `class="tx-badge `); n != len(badgeRe.FindAllString(out, -1)) || n != 6 {
				t.Errorf("%d badge elements, %d of the badge's markup, want 6", n, len(badgeRe.FindAllString(out, -1)))
			}
			if n := strings.Count(out, "badge-sm"); n != 6 {
				t.Errorf("%d pills, but 6 badges: a pill of another markup", n)
			}
			for _, m := range badgeRe.FindAllStringSubmatch(out, -1) {
				if whole := strings.TrimSpace(m[6] + " " + m[7]); m[5] != whole {
					t.Errorf("title %q, the words %q", m[5], whole)
				}
			}
			for _, old := range []string{"bg-green-500/10", "data-vault-progress-badge", "whitespace-nowrap bg-"} {
				if strings.Contains(out, old) {
					t.Errorf("the old pill's %q is still on the page", old)
				}
			}
			// The icons, once, and every one a badge uses among them.
			partial, err := os.ReadFile("../../templates/partials/status/badge.html")
			if err != nil {
				t.Fatal(err)
			}
			icons := strings.Count(string(partial), `<symbol id="tx-b-`)
			if n := strings.Count(out, `<symbol id="tx-b-`); n != icons || strings.Count(out, `class="tx-sprite `) != 1 {
				t.Errorf("%d badge icons in %d sprites, want the %d once", n, strings.Count(out, `class="tx-sprite `), icons)
			}
			for _, m := range badgeRe.FindAllStringSubmatch(out, -1) {
				if !strings.Contains(out, `<symbol id="tx-b-`+m[4]+`"`) {
					t.Errorf("icon %s is not on the page", m[4])
				}
			}
			if regexp.MustCompile(`tx-badge[^>]*\sid=|badge-text" id=`).MatchString(out) {
				t.Error("a badge with an id")
			}
		})
	}
}

// fixturePledges is a table with one pledge of each status -- one being
// vaulted (a live row), one vaulted, one that lost its backing and expires
// -- and a second live row, last: two streams at once, each drawing into its
// own row (lib/vaultProgress.test.js).
func fixturePledges() *PledgeListData {
	loading := &vaultModels.Resource{ResourceID: loadingHash, Name: "loading", Funded: true}
	vaulted := &vaultModels.Resource{ResourceID: vaultedHash, Name: "vaulted", Funded: true, Vaulted: true}
	expiring := &vaultModels.Resource{ResourceID: expiringHash, Name: "expiring"}
	second := &vaultModels.Resource{ResourceID: secondHash, Name: "second", Funded: true}
	return &PledgeListData{Pledges: []PledgeDisplay{
		{ResourceID: loadingHash, Resource: loading, Amount: 3.5, Funded: true, ShowProgress: true, IsFrozen: true},
		{ResourceID: vaultedHash, Resource: vaulted, Amount: 6.5, Funded: true},
		{ResourceID: expiringHash, Resource: expiring, Amount: 2, ExpiresIn: 5 * time.Hour},
		{ResourceID: secondHash, Resource: second, Amount: 1, Funded: true, ShowProgress: true},
	}}
}

const (
	expiringHash = "c9e15763f722f23e98a29decdfae341b98d53056"
	secondHash   = "5d0c1a9e3b7f2e4a6c8d0f1b3a5c7e9f2b4d6a8c"
)

const vaultFixtureRegen = `UPDATE_FIXTURES=1 go test ` +
	`-ldflags '-X google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=ignore' ` +
	`./handlers/vault/ -run TestVaultPageFixtureIsCurrent`

// The page's markup assets/src/js/lib/vaultProgress.test.js runs
// app/vault/progress.js against, from the real template. Committed, because
// `npm test` must not need a Go toolchain; generated, because a hand-written
// copy would keep passing after the template changed.
func TestVaultPageFixtureIsCurrent(t *testing.T) {
	// A constant token: the fixture must not change every second.
	tpl := parseVaultWith(t, func(string) string { return "status-token" })
	var page bytes.Buffer
	if err := tpl.ExecuteTemplate(&page, "main", vaultView{Lang: "ru", CSRF: "csrf-token", Data: fixturePledges()}); err != nil {
		t.Fatal(err)
	}
	// The script tag is the test's to place (lib/av registers against the
	// element holding it).
	body := strings.Replace(page.String(), `<script src="/assets/vault/progress.js"></script>`, "", 1)
	got := []byte("<!-- Generated by handlers/vault TestVaultPageFixtureIsCurrent — do not edit.\n     " + vaultFixtureRegen + " -->\n" + body + "\n")
	const path = "../../assets/src/js/lib/__fixtures__/vault-page.html"
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

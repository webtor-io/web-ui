package vault

import (
	"bytes"
	"html"
	"html/template"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	wr "github.com/webtor-io/web-ui/handlers/resource"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/vault"
	"github.com/webtor-io/web-ui/services/web"
)

const testSecret = "vault-render-test-secret"

const (
	loadingHash = "4897aacef168307328e121694dcdb8407e6d62cc"
	vaultedHash = "08ada5a7a6183aae1e09d831df6748d566095a10"
)

// parseVault parses vault/index.html with the functions the page uses;
// statusToken is the resource helper's, signing with testSecret.
func parseVault(t *testing.T) *template.Template {
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
		"statusToken": wr.NewHelper(testSecret).StatusToken,
	}).ParseFiles("../../templates/views/vault/index.html")
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

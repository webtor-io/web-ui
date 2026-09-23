package template

import (
	"bytes"
	"html/template"
	"os"
	"regexp"
	"strings"
	"testing"
	"text/template/parse"

	hc "github.com/webtor-io/web-ui/handlers/common"
	"github.com/webtor-io/web-ui/services/i18n"
)

const shareDomain = "https://webtor.io"

// headBlocks are the view blocks the layout's <head> renders.
var headBlocks = []string{"title", "description", "canonical", "og"}

// shareHead renders the <head> of templates/layouts/main.html the way a page
// gets it: the layout, the og_card partial, and the head blocks of one view
// (or none — the layout's defaults, as /about and /donate get them).
//
// The view is parsed without checking that its functions exist and only its
// head blocks are grafted onto the layout, so the page body — and the
// hundred helpers it calls — needs no stubs. The functions the head does
// call are stubbed below under their real names.
func shareHead(t *testing.T, view string, data any) string {
	t.Helper()
	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatalf("locales: %v", err)
	}
	defer locales.Close()
	tr := i18n.NewHelper(i18n.New(locales.FS()))

	funcs := template.FuncMap{
		"t":                      tr.T,
		"domain":                 func() string { return shareDomain },
		"asset":                  func(p string) template.HTML { return "" },
		"hrefLangs":              func(p string) []struct{ Lang, URL string } { return nil },
		"useUmami":               func() bool { return false },
		"umamiConfig":            func() any { return nil },
		"json":                   func(v any) template.JS { return "{}" },
		"useActionTurnstile":     func() bool { return false },
		"actionTurnstileSiteKey": func() string { return "" },
		"useSuperTokens":         func() bool { return false },
		"hasAuth":                func(any) bool { return false },
		// index.html
		"seoFriendly":  func(s string) string { return s },
		"canonicalURL": func(lang, p string) string { return shareDomain + p },
		// resource/get.html
		"hasEnrichment":    func(any) bool { return false },
		"has":              func(any, string) bool { return false },
		"getEnrichedTitle": func(any) string { return "" },
		"hasEnrichedYear":  func(any) bool { return false },
		"getEnrichedYear":  func(any) int { return 0 },
		"getEnrichedPlot":  func(any) string { return "" },
		"isEnrichedMovie":  func(any) bool { return false },
		"getOGImageURL":    func(any) string { return "/lib/poster/abc/og.jpg" },
	}

	layout, err := os.ReadFile("../../templates/layouts/main.html")
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := template.New("main.html").Funcs(funcs).Parse(string(layout))
	if err != nil {
		t.Fatalf("parse layout: %v", err)
	}
	card, err := os.ReadFile("../../templates/partials/og_card.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tpl.Parse(string(card)); err != nil {
		t.Fatalf("parse og_card: %v", err)
	}
	if _, err := tpl.Parse(`{{ define "title" }}Page{{ end }}{{ define "head_extra" }}{{ end }}` +
		`{{ define "nav" }}{{ end }}{{ define "footer" }}{{ end }}{{ define "main" }}{{ end }}` +
		`{{ define "open_instance_banner" }}{{ end }}{{ define "lang_suggest_banner" }}{{ end }}`); err != nil {
		t.Fatal(err)
	}
	if view != "" {
		src, err := os.ReadFile("../../templates/views/" + view)
		if err != nil {
			t.Fatal(err)
		}
		p := parse.New(view)
		p.Mode = parse.SkipFuncCheck
		trees := map[string]*parse.Tree{}
		if _, err := p.Parse(string(src), "", "", trees); err != nil {
			t.Fatalf("parse %s: %v", view, err)
		}
		for _, name := range headBlocks {
			if tree, ok := trees[name]; ok {
				if _, err := tpl.AddParseTree(name, tree); err != nil {
					t.Fatalf("graft %s: %v", name, err)
				}
			}
		}
	}

	ctx := map[string]any{"Lang": "en", "Path": "/", "CSRF": "", "SessionID": "", "Data": data}
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "main.html", ctx); err != nil {
		t.Fatalf("render %s: %v", view, err)
	}
	out := buf.String()
	end := strings.Index(out, "</head>")
	if end < 0 {
		t.Fatalf("no </head> in:\n%s", out)
	}
	return out[:end]
}

func metaContent(head, attr, name string) []string {
	re := regexp.MustCompile(`<meta ` + attr + `="` + regexp.QuoteMeta(name) + `" content="([^"]*)">`)
	var out []string
	for _, m := range re.FindAllStringSubmatch(head, -1) {
		out = append(out, m[1])
	}
	return out
}

// TestShareCardInEveryPreview: every page without an image of its own shares
// the brand card — the homepage, the tool pages and the layout default that
// /about, /donate and the rest get — as a large card with its size and alt
// declared. The favicon used to be the og:image everywhere, and a vector icon
// is not a preview any platform renders.
func TestShareCardInEveryPreview(t *testing.T) {
	tool := hc.Tools[0]
	cases := []struct {
		name, view string
		data       any
		// ogURL tells the view's own og block from the layout default: it
		// proves the head under test is the one the page really renders.
		ogURL []string
	}{
		{"homepage", "index.html", map[string]any{"Tool": nil}, []string{shareDomain + "/"}},
		{"tool page", "index.html", map[string]any{"Tool": tool}, []string{shareDomain + "/" + tool.Url}},
		{"layout default", "", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			head := shareHead(t, tc.view, tc.data)
			if got := metaContent(head, "property", "og:url"); strings.Join(got, " ") != strings.Join(tc.ogURL, " ") {
				t.Fatalf("og:url = %q, want %q — the head is not the page's", got, tc.ogURL)
			}
			want := map[[2]string]string{
				{"property", "og:image"}:        shareDomain + "/og-card.png",
				{"property", "og:image:type"}:   "image/png",
				{"property", "og:image:width"}:  "1200",
				{"property", "og:image:height"}: "630",
				{"name", "twitter:card"}:        "summary_large_image",
			}
			for k, v := range want {
				got := metaContent(head, k[0], k[1])
				if len(got) != 1 || got[0] != v {
					t.Errorf("%s: got %q, want exactly one %q", k[1], got, v)
				}
			}
			alt := metaContent(head, "property", "og:image:alt")
			if len(alt) != 1 || alt[0] == "" || strings.Contains(alt[0], "meta.") {
				t.Errorf("og:image:alt should be one translated line, got %q", alt)
			}
		})
	}
}

// TestResourcePageHasOneTwitterCard: a resource page brings its own
// 1200x630 card and its own twitter:card. The layout used to print a second,
// unconditional "summary" outside the og block, so the page said both.
func TestResourcePageHasOneTwitterCard(t *testing.T) {
	data := map[string]any{"Resource": map[string]any{"Name": "Some torrent", "ID": "abc"}}
	head := shareHead(t, "resource/get.html", data)
	if got := metaContent(head, "name", "twitter:card"); len(got) != 1 || got[0] != "summary_large_image" {
		t.Errorf("twitter:card = %q, want exactly one summary_large_image", got)
	}
	if got := metaContent(head, "property", "og:image"); len(got) != 1 || got[0] != shareDomain+"/lib/poster/abc/og.jpg" {
		t.Errorf("og:image = %q, want only the resource's own card", got)
	}
}

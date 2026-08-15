package template

import (
	"bytes"
	"encoding/json"
	"flag"
	"html/template"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	hc "github.com/webtor-io/web-ui/handlers/common"
)

// aboutTemplates parses every about partial the way the template manager does
// at startup, with the translation funcs echoing their key. Structure — which
// key lands where, in what markup, in what order — is what these tests are
// about; the translations themselves are covered by the locale guard in
// handlers/common.
func aboutTemplates(t *testing.T) *template.Template {
	t.Helper()
	echo := func(lang, key string, args ...interface{}) string { return key }
	funcs := template.FuncMap{
		"t":        echo,
		"tp":       echo,
		"tHTML":    func(lang, key string, args ...interface{}) template.HTML { return template.HTML(key) },
		"tpHTML":   func(lang, key string, args ...interface{}) template.HTML { return template.HTML(key) },
		"langPath": func(lang, p string) string { return p },
		"asset":    func(p string) template.HTML { return template.HTML(p) },
		// Mirrors web.Helper.WithContext: the section templates read the
		// page context as $.Ctx and their own section as $.Data.
		"withContext": func(ctx, data interface{}) interface{} {
			return map[string]interface{}{"Ctx": ctx, "Data": data}
		},
		// Only funcs the template manager actually registers (see
		// web.Helper's methods) belong here: a stub for one that does not
		// exist would let a template pass this test and panic at startup.
		"seq": func(from, to int) []int {
			var out []int
			for i := from; i <= to; i++ {
				out = append(out, i)
			}
			return out
		},
	}
	paths, err := filepath.Glob("../../templates/partials/about/*.html")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no about partials found: %v", err)
	}
	tpl := template.New("about").Funcs(funcs)
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		if _, err := tpl.Parse(string(b)); err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
	}
	// The CTAs the sections end with live outside this directory.
	if _, err := tpl.Parse(
		`{{ define "discover_cta" }}<!--discover-cta-->{{ end }}` +
			`{{ define "stremio_cta" }}<!--stremio-cta-->{{ end }}`); err != nil {
		t.Fatal(err)
	}
	return tpl
}

var updateGolden = flag.Bool("update", false, "rewrite the rendered-page snapshots in testdata/about/")

var whitespace = regexp.MustCompile(`\s+`)

// renderAbout renders one tool's about partial and normalises whitespace, so
// comparisons are about markup rather than indentation.
func renderAbout(t *testing.T, tpl *template.Template, tool hc.Tool) string {
	t.Helper()
	ctx := map[string]interface{}{
		"Lang": "en",
		"Data": map[string]interface{}{"Tool": tool, "Instruction": tool.Url},
	}
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "about_sections", ctx); err != nil {
		t.Fatalf("render /%s: %v", tool.Url, err)
	}
	return strings.TrimSpace(whitespace.ReplaceAllString(buf.String(), " "))
}

// TestAboutPartialsRender executes every tool's about partial. Parsing alone
// does not reach a bad field path or a missing sub-template — those only fail
// at execute time, on the live page, one URL at a time.
func TestAboutPartialsRender(t *testing.T) {
	tpl := aboutTemplates(t)
	for _, tool := range hc.Tools {
		t.Run(tool.Url, func(t *testing.T) {
			out := renderAbout(t, tpl, tool)
			if !strings.Contains(out, `id="how"`) {
				t.Error("no steps section — every tool page opens with one")
			}
			if strings.Contains(out, "<no value>") {
				t.Error("a field path did not resolve")
			}
		})
	}
}

// TestAboutPartialsUseTheirOwnKeys guards the copy-paste failure of a
// section-per-page layout: a page built from another page's markup keeps the
// other page's i18n prefix and silently renders its copy.
func TestAboutPartialsUseTheirOwnKeys(t *testing.T) {
	tpl := aboutTemplates(t)
	keyRe := regexp.MustCompile(`tool\.([a-zA-Z0-9]+)\.about\.`)
	for _, tool := range hc.Tools {
		t.Run(tool.Url, func(t *testing.T) {
			want := camelURL(tool.Url)
			for _, m := range keyRe.FindAllStringSubmatch(renderAbout(t, tpl, tool), -1) {
				if m[1] != want {
					t.Errorf("renders key prefix tool.%s.about.* — expected tool.%s.about.*", m[1], want)
				}
			}
		})
	}
}

func camelURL(url string) string {
	parts := strings.Split(url, "-")
	out := parts[0]
	for _, p := range parts[1:] {
		if p == "" {
			continue
		}
		out += strings.ToUpper(p[:1]) + p[1:]
	}
	return out
}

// TestAboutCopyExistsInEveryLocale checks that every key a page actually
// renders has copy in every language. A missing one prints the raw key on a
// page whose entire purpose is search traffic.
//
// The key list comes from the rendered markup rather than from a guess at
// what the section templates ask for, so adding a field to a section without
// translating it fails here rather than on the live page.
func TestAboutCopyExistsInEveryLocale(t *testing.T) {
	tpl := aboutTemplates(t)
	locs := loadLocales(t)
	keyRe := regexp.MustCompile(`tool\.[a-zA-Z0-9]+\.(?:about\.)?[a-zA-Z0-9._]+`)
	for _, tool := range hc.Tools {
		t.Run(tool.Url, func(t *testing.T) {
			seen := map[string]bool{}
			for _, k := range keyRe.FindAllString(renderAbout(t, tpl, tool), -1) {
				k = strings.TrimSuffix(k, ".")
				if seen[k] {
					continue
				}
				seen[k] = true
				for lang, d := range locs {
					if strings.TrimSpace(d[k]) == "" {
						t.Errorf("%s missing in locales/%s.json", k, lang)
					}
				}
			}
			if len(seen) < 10 {
				t.Errorf("only %d keys rendered — the page is probably empty", len(seen))
			}
		})
	}
}

func loadLocales(t *testing.T) map[string]map[string]string {
	t.Helper()
	paths, err := filepath.Glob("../../locales/??.json")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no locales found: %v", err)
	}
	out := map[string]map[string]string{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		var d map[string]string
		if err := json.Unmarshal(b, &d); err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		out[strings.TrimSuffix(filepath.Base(p), ".json")] = d
	}
	return out
}

// TestAboutSnapshots compares the rendered markup against snapshots in
// testdata. Run with -update after an intentional change and read the diff:
// these pages are search-traffic landing pages, and a refactor of the shared
// section templates is exactly the change that can quietly alter all 19.
func TestAboutSnapshots(t *testing.T) {
	tpl := aboutTemplates(t)
	dir := filepath.Join("testdata", "about")
	if *updateGolden {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, tool := range hc.Tools {
		t.Run(tool.Url, func(t *testing.T) {
			got := renderAbout(t, tpl, tool)
			path := filepath.Join(dir, tool.Url+".html")
			if *updateGolden {
				if err := os.WriteFile(path, []byte(got+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("no snapshot for /%s (run with -update): %v", tool.Url, err)
			}
			if got != strings.TrimSpace(string(want)) {
				t.Errorf("/%s renders differently from its snapshot; rerun with -update to inspect the diff", tool.Url)
			}
		})
	}
}

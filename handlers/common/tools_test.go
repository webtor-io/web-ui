package common

import (
	"encoding/json"
	"html/template"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// repoRoot is two levels up from handlers/common.
const repoRoot = "../.."

// snakeName mirrors the kebabToSnake template helper: tools.go holds kebab
// URLs, the partial file and its define use snake.
func snakeName(url string) string {
	return strings.ReplaceAll(url, "-", "_")
}

// camelName mirrors the i18n key prefix: torrent-to-mp4 → torrentToMp4.
func camelName(url string) string {
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

// aboutPartials parses every about partial into one template set, the way the
// template manager does at startup.
func aboutPartials(t *testing.T) *template.Template {
	t.Helper()
	stub := func(args ...interface{}) interface{} { return "" }
	funcs := template.FuncMap{}
	for _, name := range []string{
		"t", "tp", "tHTML", "tpHTML", "langPath", "asset", "deref", "isPaid",
		"withContext", "json", "raw", "dict", "slice", "kebabToSnake",
		"dynamicTemplate", "faqSchema", "hasPrefix", "add", "seq",
	} {
		funcs[name] = stub
	}
	paths, err := filepath.Glob(filepath.Join(repoRoot, "templates/partials/about/*.html"))
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
	return tpl
}

func locales(t *testing.T) map[string]map[string]string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(repoRoot, "locales/??.json"))
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

// TestEveryToolHasItsAboutPartial guards the failure mode that shipped once:
// a tool registered here without its partial renders a blank page on that URL
// alone. Nothing else catches it — the lookup is dynamic, so the build and the
// rest of the suite pass while the live page is empty.
func TestEveryToolHasItsAboutPartial(t *testing.T) {
	tpl := aboutPartials(t)
	for _, tool := range Tools {
		name := "about_instruction_" + snakeName(tool.Url) + "_how_to"
		if tpl.Lookup(name) == nil {
			t.Errorf("/%s has no partial defining %q — that page renders blank", tool.Url, name)
		}
	}
}

// TestEveryToolHasItsCopyInEveryLocale covers the other half: a missing key
// renders as the raw key on a page meant for search traffic.
func TestEveryToolHasItsCopyInEveryLocale(t *testing.T) {
	locs := locales(t)
	keyRe := regexp.MustCompile(`"(tool\.[a-zA-Z0-9]+\.[a-zA-Z0-9._]+)"`)

	for _, tool := range Tools {
		camel := camelName(tool.Url)
		want := map[string]bool{
			tool.Title:       true,
			tool.Benefit:     true,
			tool.Description: true,
		}
		// Plus whatever the tool's own partial asks for.
		b, err := os.ReadFile(filepath.Join(repoRoot, "templates/partials/about", snakeName(tool.Url)+".html"))
		if err == nil {
			for _, m := range keyRe.FindAllStringSubmatch(string(b), -1) {
				if strings.HasPrefix(m[1], "tool."+camel+".") {
					want[m[1]] = true
				}
			}
		}
		for lang, d := range locs {
			for k := range want {
				if strings.TrimSpace(d[k]) == "" {
					t.Errorf("/%s: %s missing in locales/%s.json", tool.Url, k, lang)
				}
			}
		}
	}
}

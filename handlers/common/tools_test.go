package common

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot is two levels up from handlers/common.
const repoRoot = "../.."

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

// TestEveryToolDeclaresItsBody guards the failure mode that shipped once: a
// tool registered here without a body renders a URL with nothing between the
// hero and the footer. It used to be possible because the body was a partial
// looked up by name at render time; now it is this list, and an empty one is
// the same blank page.
func TestEveryToolDeclaresItsBody(t *testing.T) {
	for _, tool := range Tools {
		if len(tool.Sections) == 0 {
			t.Errorf("/%s declares no sections — that page renders blank", tool.Url)
			continue
		}
		if tool.Sections[0].Kind != AboutSteps {
			t.Errorf("/%s opens with %q — every tool page opens with the steps section, which carries the #how anchor", tool.Url, tool.Sections[0].Kind)
		}
		for _, s := range tool.Sections {
			if s.Prefix != tool.AboutKey() {
				t.Errorf("/%s: section %q has prefix %q, want %q — the init stamp did not run", tool.Url, s.Key, s.Prefix, tool.AboutKey())
			}
			switch s.Kind {
			case AboutCompare:
				if len(s.Cols) != 2 {
					t.Errorf("/%s: comparison %q has %d columns, want 2", tool.Url, s.Key, len(s.Cols))
				}
			case AboutChecklist:
				if s.Items == 0 {
					t.Errorf("/%s: checklist %q lists no items", tool.Url, s.Key)
				}
			case AboutProse:
				if len(s.Paras) == 0 {
					t.Errorf("/%s: prose section %q has no paragraphs", tool.Url, s.Key)
				}
			}
		}
	}
}

// TestEveryToolHasItsCopyInEveryLocale covers the tool's own labels — the
// footer link, the tools grid, the meta description. The page body's keys are
// checked against the rendered markup in services/template, which is exact
// rather than a guess at what the template asks for.
func TestEveryToolHasItsCopyInEveryLocale(t *testing.T) {
	locs := locales(t)
	for _, tool := range Tools {
		for lang, d := range locs {
			for _, k := range []string{tool.Title, tool.Benefit, tool.Description} {
				if strings.TrimSpace(d[k]) == "" {
					t.Errorf("/%s: %s missing in locales/%s.json", tool.Url, k, lang)
				}
			}
		}
	}
}

// TestAboutKeyFollowsTheURL pins the kebab-URL/camel-key convention every
// locale key on these pages is built from.
func TestAboutKeyFollowsTheURL(t *testing.T) {
	for _, tool := range Tools {
		want := "tool." + camelName(tool.Url) + ".about"
		if got := tool.AboutKey(); got != want {
			t.Errorf("/%s: AboutKey() = %q, want %q", tool.Url, got, want)
		}
		// The title key is written out by hand; if it disagrees with the
		// derived prefix, the page body reads another tool's copy.
		if !strings.HasPrefix(tool.Title, "tool."+camelName(tool.Url)+".") {
			t.Errorf("/%s: title key %q does not match the URL", tool.Url, tool.Title)
		}
	}
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

package stremio

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// langJSPath is the client half of the same table. Discover's stream modal
// and the picker's language chips read it, the server reads Languages, and
// until 2026-09-16 the only thing keeping them equal was a comment saying
// "keep in sync".
const langJSPath = "../../assets/src/js/lib/discover/lang.js"

var jsRowRe = regexp.MustCompile(`\{\s*code:\s*'([^']*)',\s*name:\s*'([^']*)',\s*flag:\s*'([^']*)',\s*titleAliases:\s*\[([^\]]*)\]`)

type jsLang struct {
	code, name, flag string
	aliases          []string
}

func parseLangJS(t *testing.T) []jsLang {
	t.Helper()
	b, err := os.ReadFile(filepath.FromSlash(langJSPath))
	if err != nil {
		t.Fatalf("read %s: %v", langJSPath, err)
	}
	src := string(b)
	start := strings.Index(src, "const LANGUAGES = [")
	if start < 0 {
		t.Fatalf("%s: no LANGUAGES array — this test parses it, so a rename has to be made here too", langJSPath)
	}
	end := strings.Index(src[start:], "\n];")
	if end < 0 {
		t.Fatalf("%s: LANGUAGES array is not terminated by \"\\n];\"", langJSPath)
	}
	var out []jsLang
	for _, m := range jsRowRe.FindAllStringSubmatch(src[start:start+end], -1) {
		var aliases []string
		for _, a := range strings.Split(m[4], ",") {
			if a = strings.Trim(strings.TrimSpace(a), "'"); a != "" {
				aliases = append(aliases, a)
			}
		}
		out = append(out, jsLang{code: m[1], name: m[2], flag: m[3], aliases: aliases})
	}
	if len(out) == 0 {
		t.Fatalf("%s: parsed no rows — the row shape changed and this regexp did not", langJSPath)
	}
	return out
}

// TestLangJSMirrorsTheGoTable compares the two tables field by field and in
// order. They are one table serving two runtimes: the server filters
// Stremio streams by it and the client draws Discover's chips from it, so a
// row added on one side alone is a surface that disagrees with itself —
// which is what "keep in sync" in a comment bought us until this test.
//
// Excluded from the comparison: `extraFlags`, which only the client has
// (🇺🇸/🇦🇺 for English, 🇵🇹 for Portuguese, 🇲🇽/🇦🇷 for Latino), and the one
// row with an empty `code` — Latino, a release-title tag rather than a
// language anyone can choose in the settings.
func TestLangJSMirrorsTheGoTable(t *testing.T) {
	js := parseLangJS(t)
	var coded []jsLang
	var uncoded []string
	for _, l := range js {
		if l.code == "" {
			uncoded = append(uncoded, l.name)
			continue
		}
		coded = append(coded, l)
	}
	// Latino is the only row allowed to have no code. Anything else
	// without one is a row this test would otherwise silently skip.
	if len(uncoded) != 1 || uncoded[0] != "Latino" {
		t.Errorf("rows with no code: %v, want exactly [Latino]", uncoded)
	}
	if len(coded) != len(Languages) {
		t.Fatalf("%s has %d languages, Go has %d", langJSPath, len(coded), len(Languages))
	}
	for i, g := range Languages {
		j := coded[i]
		if j.code != g.Code || j.name != g.Name || j.flag != g.Flag {
			t.Errorf("row %d: js {%s %s %s} != go {%s %s %s}",
				i, j.code, j.name, j.flag, g.Code, g.Name, g.Flag)
			continue
		}
		if strings.Join(j.aliases, ",") != strings.Join(g.TitleAliases, ",") {
			t.Errorf("%s titleAliases: js %v != go %v", g.Code, j.aliases, g.TitleAliases)
		}
	}
}

// TestLangJSFlagsResolveToOneLanguageEach: the client keys its lookup map
// by flag as well as by alias, and Latino has always carried Spain's flag.
// Registration there is first-wins, so 🇪🇸 resolves to Spanish and Latino
// keeps 🇲🇽/🇦🇷 — but only the JS side can express that, which is why the
// Go table (where uniqueness is absolute, TestLanguagesAreWellFormed)
// cannot be the only place this is checked.
func TestLangJSFlagsResolveToOneLanguageEach(t *testing.T) {
	seen := map[string]string{}
	for _, l := range parseLangJS(t) {
		if len(l.aliases) == 0 {
			// Not in LANG_MAP at all, so its flag collides with nothing.
			continue
		}
		if prev, ok := seen[l.flag]; ok && prev != "Spanish" {
			t.Errorf("%s reuses %s's flag %s", l.name, prev, l.flag)
		} else if !ok {
			seen[l.flag] = l.name
		}
	}
	src, err := os.ReadFile(filepath.FromSlash(langJSPath))
	if err != nil {
		t.Fatal(err)
	}
	// The first-wins registration is what makes the exception above safe;
	// without it the later Latino row overwrites LANG_MAP['🇪🇸'].
	if !strings.Contains(string(src), "if (!(k in LANG_MAP)) LANG_MAP[k] = entry;") {
		t.Error("LANG_MAP must register the first key that claims it: Latino would otherwise shadow Spanish on 🇪🇸")
	}
}

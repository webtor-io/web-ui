package i18n

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// localeFiles reads every locales/xx.json from disk, keyed by language code.
// Metadata keys ("@foo", ARB-style translator context) are dropped: they live
// only in en.json by convention and are never registered as messages.
func localeFiles(t *testing.T) map[string]map[string]string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("..", "..", "locales", "??.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no locale files found: %v", err)
	}
	out := make(map[string]map[string]string, len(paths))
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		var d map[string]any
		if err := json.Unmarshal(b, &d); err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		msgs := make(map[string]string, len(d))
		for k, v := range d {
			if strings.HasPrefix(k, "@") {
				continue
			}
			switch val := v.(type) {
			case string:
				msgs[k] = val
			case map[string]any:
				// A pluralised message: CLDR forms keyed one/few/many/other.
				// Flattened to one string for the checks below; every form
				// must be non-empty and "other" must exist, because go-i18n
				// falls back to it for counts the language does not split.
				if _, ok := val["other"]; !ok {
					t.Errorf("%s: plural key %q has no \"other\" form", filepath.Base(p), k)
				}
				var parts []string
				for form, fv := range val {
					fs, _ := fv.(string)
					if fs == "" {
						t.Errorf("%s: plural key %q form %q is empty", filepath.Base(p), k, form)
					}
					parts = append(parts, form+"="+fs)
				}
				msgs[k] = strings.Join(parts, "|")
			default:
				t.Errorf("%s: key %q has unsupported value type %T", filepath.Base(p), k, v)
			}
		}
		out[strings.TrimSuffix(filepath.Base(p), ".json")] = msgs
	}
	if _, ok := out[DefaultLang]; !ok {
		t.Fatalf("locales/%s.json not found", DefaultLang)
	}
	return out
}

// TestEveryLocaleHasTheSameKeysAsEnglish is the repo-wide guard, not a guard
// for one branch: en.json is the source of truth, and a key added there
// without the other ten is a shipped defect (the fallback keeps it readable,
// but it is still English text in a non-English page).
//
// An "extra" key is a defect too — usually a rename that landed in en.json and
// left the old spelling behind in a translation, where it will never be read.
func TestEveryLocaleHasTheSameKeysAsEnglish(t *testing.T) {
	locs := localeFiles(t)
	en := locs[DefaultLang]

	for lang, d := range locs {
		if lang == DefaultLang {
			continue
		}
		var missing, extra []string
		for k := range en {
			if _, ok := d[k]; !ok {
				missing = append(missing, k)
			}
		}
		for k := range d {
			if _, ok := en[k]; !ok {
				extra = append(extra, k)
			}
		}
		sort.Strings(missing)
		sort.Strings(extra)
		if len(missing) > 0 {
			t.Errorf("locales/%s.json is missing %d key(s) present in %s.json: %s",
				lang, len(missing), DefaultLang, strings.Join(missing, ", "))
		}
		if len(extra) > 0 {
			t.Errorf("locales/%s.json has %d key(s) absent from %s.json: %s",
				lang, len(extra), DefaultLang, strings.Join(extra, ", "))
		}
	}
}

// TestNoLocaleHasAnEmptyTranslation catches the other way a key can be
// present-but-useless: added to the file with an empty string to satisfy a
// key-set check.
func TestNoLocaleHasAnEmptyTranslation(t *testing.T) {
	for lang, d := range localeFiles(t) {
		for k, v := range d {
			if strings.TrimSpace(v) == "" {
				t.Errorf("locales/%s.json: %s is empty", lang, k)
			}
		}
	}
}

// breakableUnit matches a number (or a template placeholder) followed by a
// plain space and an abbreviated unit. The rule (docs/i18n.md, "Numbers and
// units"): that space is a no-break space, U+00A0, so "43 s" and "1.0 MB"
// never split across lines on a phone. Whole words ("6 seeders") are not
// units and may wrap.
// Long units are unambiguous, so a Turkish suffix glued to them with an
// apostrophe ("50 Mbps'ye") still counts as the unit.
var breakableUnitLong = regexp.MustCompile(`(\}\}|[0-9]) (?:sec|сек|дн\.|d\.|min|мин|godz\.|Std\.|Min\.|Tg\.|MB|GB|kB|KB|TB|МБ|ГБ|КБ|ТБ|Mbps|Mbit/s|Мбит/с|MB/s|Мб/с|Mo|Go|To|ko|Мбит)(?:'\p{L}+)?(?:$|[\s.,;:!?)\]<»"”])`)

// One- and two-letter units are also ordinary words in some locales, so they
// need a hard boundary and no suffix rule.
var breakableUnitShort = regexp.MustCompile(`(\}\}|[0-9]) (?:s|с|sn|h|ч|d|j|g|u|dk|sa)(?:$|[\s.,;:!?)\]<»"”])`)

func breakableUnit(v string) string {
	if m := breakableUnitLong.FindString(v); m != "" {
		return m
	}
	return breakableUnitShort.FindString(v)
}

// Negative control for the guard above: the vocabulary has to actually match
// the units we ship. Every unit added to a locale belongs here — the guard
// stays silent about a unit it does not know, which is how a whole set of new
// ones slipped through unnoticed before.
func TestUnitGuardKnowsTheUnitsWeShip(t *testing.T) {
	for _, s := range []string{
		"{{.M}} min", "{{.M}} мин", "{{.H}} h", "{{.H}} ч", "{{.H}} Std.", "{{.M}} Min.",
		"{{.D}} d", "{{.D}} d.", "{{.D}} дн.", "{{.D}} j", "{{.D}} g", "{{.H}} u", "{{.H}} godz.", "{{.D}} Tg.",
		"{{.M}} dk", "{{.H}} sa", "50 Mbps", "50 Mbit/s", "50 Мбит/с", "50 Mbps'ye kadar",
		"250 GB", "1 TB", "250 ГБ", "1 ТБ", "250 Go)", "1 To)", "43 s",
	} {
		if breakableUnit(s) == "" {
			t.Errorf("the unit guard does not know %q — a locale could ship it with a breakable space and stay green", s)
		}
	}
	// ...and it must not fire on whole words, which keep a normal space.
	for _, s := range []string{"6 seeders", "7 days", "7 дней", "7 gün", "3 mesi gratis", "2 dny zdarma"} {
		if m := breakableUnit(s); m != "" {
			t.Errorf("the unit guard fired on a whole word: %q in %q", m, s)
		}
	}
}

func TestUnitsFollowTheirNumberWithANoBreakSpace(t *testing.T) {
	for lang, d := range localeFiles(t) {
		for k, v := range d {
			if m := breakableUnit(v); m != "" {
				t.Errorf("locales/%s.json: %s has a breakable space before a unit (%q) — use U+00A0", lang, k, m)
			}
		}
	}
}

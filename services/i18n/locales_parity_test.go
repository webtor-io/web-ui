package i18n

import (
	"encoding/json"
	"os"
	"path/filepath"
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
		var d map[string]string
		if err := json.Unmarshal(b, &d); err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		msgs := make(map[string]string, len(d))
		for k, v := range d {
			if strings.HasPrefix(k, "@") {
				continue
			}
			msgs[k] = v
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

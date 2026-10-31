package genre

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFromTMDB(t *testing.T) {
	g := func(id any, name string) map[string]any { return map[string]any{"id": id, "name": name} }
	for _, c := range []struct {
		name string
		raw  any
		want []string
	}{
		{"film", []any{g(28.0, "Action"), g(878.0, "Science Fiction")}, []string{"action", "scifi"}},
		{"tv combos split", []any{g(10759.0, "Action & Adventure"), g(10765.0, "Sci-Fi & Fantasy"), g(10768.0, "War & Politics")},
			[]string{"action", "adventure", "scifi", "fantasy", "war", "politics"}},
		{"dedup keeps first appearance", []any{g(12.0, "Adventure"), g(10759.0, "Action & Adventure"), g(28.0, "Action")},
			[]string{"adventure", "action"}},
		{"unknown id skipped", []any{g(1.0, "Nope"), g(18.0, "Drama")}, []string{"drama"}},
		{"malformed entries skipped", []any{"Drama", nil, map[string]any{"name": "Drama"}, g("18", "Drama"), g(35.0, "Comedy")}, []string{"comedy"}},
		{"empty list", []any{}, []string{}},
		{"missing", nil, []string{}},
		{"not a list", map[string]any{"id": 18.0}, []string{}},
	} {
		got := FromTMDB(c.raw)
		if got == nil {
			t.Errorf("%s: nil, want a non-nil slice (nil is stored as NULL, \"not derived\")", c.name)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestEveryTMDBGenreIsASlug(t *testing.T) {
	for id, slugs := range tmdbIDs {
		for _, s := range slugs {
			if !IsSlug(s) {
				t.Errorf("tmdb genre %d maps to %q, not in Slugs", id, s)
			}
		}
	}
	if IsSlug("Action") || IsSlug("") {
		t.Error("IsSlug accepts a non-slug")
	}
}

// The vocabulary and its labels are one list in two places: a slug without
// a label renders as its key, and a label without a slug is never shown.
func TestEverySlugHasALabel(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "locales", "en.json"))
	if err != nil {
		t.Fatal(err)
	}
	var en map[string]any
	if err := json.Unmarshal(b, &en); err != nil {
		t.Fatal(err)
	}
	for _, s := range Slugs {
		if _, ok := en["genre."+s]; !ok {
			t.Errorf("locales/en.json has no genre.%s", s)
		}
	}
	for k := range en {
		s, ok := strings.CutPrefix(k, "genre.")
		if !ok || s == "all" || s == "label" || s == "yearLabel" {
			continue
		}
		if !IsSlug(s) {
			t.Errorf("locales/en.json has %s, but %q is not in Slugs", k, s)
		}
	}
}

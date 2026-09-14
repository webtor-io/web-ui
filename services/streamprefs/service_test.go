package streamprefs

import (
	"context"
	"strings"
	"testing"
)

func TestResolvePreferred(t *testing.T) {
	cases := []struct{ setting, ui, want string }{
		{"pt", "en", "pt"}, {"", "ru", "ru"}, {"xx", "de", "de"}, {" uk ", "en", "uk"}, {"", "pt-BR", "pt"}, {"", "", ""},
	}
	for _, c := range cases {
		if got := ResolvePreferred(c.setting, c.ui); got != c.want {
			t.Errorf("ResolvePreferred(%q,%q)=%q want %q", c.setting, c.ui, got, c.want)
		}
	}
}

func TestIsAdultResourceNilSafe(t *testing.T) {
	var s *Service
	if s.IsAdultResource(context.Background(), "abc") {
		t.Fatal("nil service must never classify as adult")
	}
	if (&Service{}).IsAdultResource(context.Background(), "abc") {
		t.Fatal("no DB → not adult")
	}
}

func TestCastNamesFromCredits(t *testing.T) {
	md := map[string]any{"credits": map[string]any{"cast": []any{
		map[string]any{"name": "Rosalind Russell"}, map[string]any{"name": "Cary Grant"}, map[string]any{"name": ""},
	}}}
	got := castNamesFromMetadata(md, 5)
	if len(got) != 2 || got[0] != "Rosalind Russell" || got[1] != "Cary Grant" {
		t.Fatalf("got %v", got)
	}
	if got := castNamesFromMetadata(map[string]any{}, 5); len(got) != 0 {
		t.Fatalf("no credits → empty, got %v", got)
	}
}

// TestCastNamesAreCappedPerName bounds one name, not just their number.
// The glossary is sent as a `names=` query parameter on every translated
// subtitle URL; 30 names is a sane count, but TMDB credits are free text
// and one absurd entry can push the URL past what the proxy chain will
// carry. The cap is on runes, not bytes: a 40-character Cyrillic or CJK
// name must survive whole rather than be cut mid-character.
func TestCastNamesAreCappedPerName(t *testing.T) {
	long := strings.Repeat("a", 60)
	cyrillic := strings.Repeat("я", 40)
	md := map[string]any{"credits": map[string]any{"cast": []any{
		map[string]any{"name": long},
		map[string]any{"name": cyrillic},
		map[string]any{"name": "Cary Grant"},
	}}}
	got := castNamesFromMetadata(md, 5)
	if len(got) != 3 {
		t.Fatalf("got %v", got)
	}
	if n := len([]rune(got[0])); n != castNameMaxRunes {
		t.Errorf("an over-long name is cut to %d runes, got %d (%q)", castNameMaxRunes, n, got[0])
	}
	if got[1] != cyrillic {
		t.Errorf("a 40-rune name must survive whole: %q", got[1])
	}
	if got[2] != "Cary Grant" {
		t.Errorf("an ordinary name is untouched: %q", got[2])
	}
}

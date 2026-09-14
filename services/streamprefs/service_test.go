package streamprefs

import (
	"context"
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

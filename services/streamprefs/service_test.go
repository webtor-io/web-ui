package streamprefs

import (
	"context"
	"net/url"
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
	// No metadata store configured is an answer ("nothing to know"), not
	// a failed lookup: the hint gate must not read it as unknown.
	if adult, known := (&Service{}).AdultResource(context.Background(), "abc"); adult || !known {
		t.Fatalf("no store configured: want not adult and known, got adult=%v known=%v", adult, known)
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

// TestCastNamesAreCappedInAggregate is the other half of the bound. The
// per-name cap and the count together still allow 30 x 40 CJK runes = 3600
// bytes, which url.Values.Encode turns into ~10.8 KB on a URL that is
// already signed -- past nginx's 8 KB default header-line budget, so the
// <track> 414s and the viewer gets subtitle-translate-error {code:414} with
// nothing shorter to fall back to.
func TestCastNamesAreCappedInAggregate(t *testing.T) {
	// 40 CJK runes is 120 bytes of UTF-8 and 360 bytes percent-encoded:
	// the worst case the per-name cap allows.
	name := strings.Repeat("渡", castNameMaxRunes)
	var cast []any
	for i := 0; i < 30; i++ {
		cast = append(cast, map[string]any{"name": name})
	}
	got := castNamesFromMetadata(map[string]any{"credits": map[string]any{"cast": cast}}, 30)
	if len(got) == 0 {
		t.Fatal("the glossary must not be emptied, only bounded")
	}
	if n := len(url.QueryEscape(strings.Join(got, ","))); n > castNamesMaxEncodedBytes {
		t.Errorf("encoded glossary is %d bytes, cap is %d (%d names)", n, castNamesMaxEncodedBytes, len(got))
	}
	// And it is the aggregate that stopped it, not the count: 30 were on
	// offer and the cap is what the answer is short of.
	if len(got) >= 30 {
		t.Errorf("all %d CJK names fit, so nothing was bounded", len(got))
	}

	// A short name behind a long one still gets in: the cap skips what does
	// not fit rather than truncating the list at the first oversized entry.
	cast = append(cast, map[string]any{"name": "Cary Grant"})
	got = castNamesFromMetadata(map[string]any{"credits": map[string]any{"cast": cast}}, 31)
	if len(got) == 0 || got[len(got)-1] != "Cary Grant" {
		t.Errorf("a short name behind oversized ones must still fit: %v", got)
	}

	// The ordinary case is untouched: real credits are nowhere near the cap.
	plain := []any{}
	for i := 0; i < 30; i++ {
		plain = append(plain, map[string]any{"name": "Cary Grant"})
	}
	if got := castNamesFromMetadata(map[string]any{"credits": map[string]any{"cast": plain}}, 30); len(got) != 30 {
		t.Errorf("30 ordinary names must all survive, got %d", len(got))
	}
}

package action

import "testing"

func li(id, lang, provider string, def bool) ListItem {
	return ListItem{ID: id, SrcLang: lang, Provider: provider, Default: def, Badge: badgeFor(provider, false)}
}

func TestSubtitleLangGroupsOrdersActiveThenPreferredThenCount(t *testing.T) {
	h := NewHelper()
	lis := []ListItem{
		{ID: "none", Label: "None"},
		li("a", "en", "MediaProbe", false),
		li("b", "en", "OpenSubtitles", false),
		li("c", "en", "ExportTag", false),
		li("d", "ru", "OpenSubtitles", true),
		li("e", "ru", "UserSubtitle", false),
		li("f", "de", "OpenSubtitles", false),
		li("g", "", "ExportTag", false),
	}
	row := h.SubtitleLangGroups(lis, "de")

	// "none" is not a language and never makes a chip.
	if got := len(row.Groups); got != 4 {
		t.Fatalf("got %d groups, want 4: %+v", got, row.Groups)
	}
	want := []string{"ru", "de", "en", "und"}
	for i, w := range want {
		if row.Groups[i].Lang != w {
			t.Errorf("group %d = %q, want %q (%+v)", i, row.Groups[i].Lang, w, row.Groups)
		}
	}
	if !row.Groups[0].Active {
		t.Error("the group holding the default track must be Active")
	}
	if row.Groups[2].Count != 3 {
		t.Errorf("en count = %d, want 3", row.Groups[2].Count)
	}
	if row.Expanded != "ru" {
		t.Errorf("Expanded = %q, want %q", row.Expanded, "ru")
	}
	if row.Overflow != 0 {
		t.Errorf("Overflow = %d, want 0", row.Overflow)
	}
}

// Subtitles off (the "None" item is the default) is not "no language
// chosen": the row still has to open on something, and the preferred
// language is the only sensible guess.
func TestSubtitleLangGroupsExpandsPreferredWhenNothingIsActive(t *testing.T) {
	h := NewHelper()
	lis := []ListItem{
		{ID: "none", Label: "None", Default: true},
		li("a", "en", "MediaProbe", false),
		li("b", "en", "OpenSubtitles", false),
		li("c", "de", "OpenSubtitles", false),
	}
	row := h.SubtitleLangGroups(lis, "de")
	if row.Expanded != "de" {
		t.Errorf("Expanded = %q, want %q", row.Expanded, "de")
	}
	if row.Groups[0].Lang != "de" {
		t.Errorf("preferred language must sort first when nothing is active, got %+v", row.Groups)
	}
	for _, g := range row.Groups {
		if g.Active {
			t.Errorf("no group may be Active when the default is None: %+v", g)
		}
	}
}

// maxVisibleLangChips is 6 (controller ruling on the brief's 4 — the design
// was redrawn wider). Six single-track languages plus the active one (added
// last, so insertion order alone would push it into the overflow) proves
// the Active-first rule is what keeps it visible.
func TestSubtitleLangGroupsOverflowNeverHidesTheActiveLanguage(t *testing.T) {
	h := NewHelper()
	lis := []ListItem{{ID: "none", Label: "None"}}
	for _, l := range []string{"en", "de", "fr", "es", "it", "pl"} {
		lis = append(lis, li("x-"+l, l, "OpenSubtitles", false))
	}
	lis = append(lis, li("y", "cs", "OpenSubtitles", true))

	row := h.SubtitleLangGroups(lis, "")
	if row.Groups[0].Lang != "cs" || row.Groups[0].Overflow {
		t.Fatalf("active language must be first and visible, got %+v", row.Groups[0])
	}
	if row.Overflow != 1 {
		t.Errorf("Overflow = %d, want 1 (7 groups, 6 visible)", row.Overflow)
	}
	for i, g := range row.Groups {
		if want := i >= 6; g.Overflow != want {
			t.Errorf("group %d (%s) Overflow = %v, want %v", i, g.Lang, g.Overflow, want)
		}
	}
}

// The preferred language gets the same always-visible guarantee as the
// active one, and the two guarantees have to hold together: with seven
// single-track languages ahead of both the active track and the preferred
// language in insertion order, only the front-of-queue treatment for both
// keeps them out of the "+N" overflow — six visible slots is not enough
// room for either to land there by luck.
func TestSubtitleLangGroupsOverflowNeverHidesActiveOrPreferred(t *testing.T) {
	h := NewHelper()
	lis := []ListItem{{ID: "none", Label: "None"}}
	for _, l := range []string{"en", "de", "fr", "es", "it", "pl", "nl"} {
		lis = append(lis, li("x-"+l, l, "OpenSubtitles", false))
	}
	lis = append(lis, li("y", "cs", "OpenSubtitles", true))
	lis = append(lis, li("z", "fi", "OpenSubtitles", false))

	row := h.SubtitleLangGroups(lis, "fi")
	if row.Groups[0].Lang != "cs" || row.Groups[0].Overflow {
		t.Fatalf("active language must be first and visible, got %+v", row.Groups[0])
	}
	if row.Groups[1].Lang != "fi" || row.Groups[1].Overflow {
		t.Fatalf("preferred language must be visible even though it was pushed by insertion order, got %+v", row.Groups[1])
	}
	if row.Overflow != 3 {
		t.Errorf("Overflow = %d, want 3 (9 groups, 6 visible)", row.Overflow)
	}
}

// A forced track's Badge is "forced", which says what kind of track it is,
// not where it came from. The origin code must still be the provider's.
func TestOriginCodeAndPropertyTags(t *testing.T) {
	h := NewHelper()
	cases := []struct {
		li   ListItem
		code string
		key  string
		tags []string
	}{
		{ListItem{Provider: "MediaProbe", Badge: "embedded"}, "EM", "action.stream.badge.embedded", nil},
		{ListItem{Provider: "MediaProbe", Badge: "forced", Forced: true}, "EM", "action.stream.badge.embedded", []string{"forced"}},
		{ListItem{Provider: "ExportTag", Badge: "sidecar"}, "IN", "action.stream.badge.sidecar", nil},
		{ListItem{Provider: "External", Badge: "sidecar"}, "IN", "action.stream.badge.sidecar", nil},
		{ListItem{Provider: "OpenSubtitles", Badge: "os"}, "OS", "action.stream.badge.os", nil},
		{ListItem{Provider: "UserSubtitle", Badge: "user"}, "MY", "action.stream.badge.user", nil},
		{ListItem{Provider: "Translated", Badge: "ai"}, "AI", "action.stream.badge.ai", nil},
		{ListItem{ID: "none"}, "", "", nil},
	}
	for _, c := range cases {
		if got := h.OriginCode(c.li); got != c.code {
			t.Errorf("OriginCode(%+v) = %q, want %q", c.li, got, c.code)
		}
		if got := h.OriginKey(c.li); got != c.key {
			t.Errorf("OriginKey(%+v) = %q, want %q", c.li, got, c.key)
		}
		got := h.PropertyTags(c.li)
		if len(got) != len(c.tags) {
			t.Errorf("PropertyTags(%+v) = %v, want %v", c.li, got, c.tags)
			continue
		}
		for i := range got {
			if got[i] != c.tags[i] {
				t.Errorf("PropertyTags(%+v) = %v, want %v", c.li, got, c.tags)
			}
		}
	}
	if got := h.OriginCodeForBadge("sidecar"); got != "IN" {
		t.Errorf("OriginCodeForBadge(sidecar) = %q, want IN", got)
	}
	if got := h.OriginCodeForBadge("forced"); got != "" {
		t.Errorf("OriginCodeForBadge(forced) = %q, want \"\" — forced is a property, not an origin", got)
	}
}

func TestAudioSuffixDropsTheLanguageNameItWouldRepeat(t *testing.T) {
	h := NewHelper()
	if got := h.AudioSuffix(ListItem{SrcLang: "en", Label: "English"}); got != "" {
		t.Errorf("AudioSuffix = %q, want \"\" — the chip already says English", got)
	}
	if got := h.AudioSuffix(ListItem{SrcLang: "ru", Label: "Dub"}); got != "Dub" {
		t.Errorf("AudioSuffix = %q, want Dub", got)
	}
	if got := h.AudioSuffix(ListItem{SrcLang: "", Label: "Audio #1"}); got != "Audio #1" {
		t.Errorf("AudioSuffix = %q, want Audio #1", got)
	}
}

package action

import "testing"

func li(id, lang, provider string, def bool) ListItem {
	return ListItem{ID: id, SrcLang: lang, Provider: provider, Default: def, Badge: badgeFor(provider, false)}
}

func TestSubtitleLangGroupsOrdersPreferredThenActiveThenCount(t *testing.T) {
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
	// Preferred first, whatever is playing (owner, 2026-09-15): the row
	// opens where the viewer's own language is, and the track playing keeps
	// the dot on its chip instead of the front slot.
	want := []string{"de", "ru", "en", "und"}
	for i, w := range want {
		if row.Groups[i].Lang != w {
			t.Errorf("group %d = %q, want %q (%+v)", i, row.Groups[i].Lang, w, row.Groups)
		}
	}
	if !row.Groups[1].Active {
		t.Error("the group holding the default track must be Active")
	}
	if row.Groups[0].Active {
		t.Error("the preferred group holds no default track and must not be Active")
	}
	if row.Groups[2].Count != 3 {
		t.Errorf("en count = %d, want 3", row.Groups[2].Count)
	}
	if row.Expanded != "de" {
		t.Errorf("Expanded = %q, want %q", row.Expanded, "de")
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

// An empty preferredLang must not resolve to "und" and collide with the
// real Unknown-language group: with no active track and no preference, the
// group with the most tracks must win the tie-break, not whichever group
// happens to be untagged.
func TestSubtitleLangGroupsEmptyPreferredDoesNotCollideWithUnknown(t *testing.T) {
	h := NewHelper()
	lis := []ListItem{
		{ID: "none", Label: "None"},
		li("a", "", "ExportTag", false),
		li("b", "en", "OpenSubtitles", false),
		li("c", "en", "MediaProbe", false),
		li("d", "en", "UserSubtitle", false),
	}
	row := h.SubtitleLangGroups(lis, "")
	if row.Expanded != "en" {
		t.Errorf("Expanded = %q, want %q", row.Expanded, "en")
	}
	if row.Groups[0].Lang != "en" {
		t.Errorf("the larger group must sort first when there is no active track and no preference, got %+v", row.Groups)
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
	if row.Groups[0].Lang != "fi" || row.Groups[0].Overflow {
		t.Fatalf("preferred language must be first and visible, got %+v", row.Groups[0])
	}
	if row.Groups[1].Lang != "cs" || row.Groups[1].Overflow {
		t.Fatalf("active language must be visible even though it was pushed by insertion order, got %+v", row.Groups[1])
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
		{ListItem{Provider: "MediaProbe", Badge: "embedded"}, "EM", "action.stream.origin.em", nil},
		{ListItem{Provider: "MediaProbe", Badge: "forced", Forced: true}, "EM", "action.stream.origin.em", []string{"forced"}},
		{ListItem{Provider: "ExportTag", Badge: "sidecar"}, "IN", "action.stream.origin.in", nil},
		{ListItem{Provider: "External", Badge: "sidecar"}, "IN", "action.stream.origin.in", nil},
		{ListItem{Provider: "OpenSubtitles", Badge: "os"}, "OS", "action.stream.origin.os", nil},
		{ListItem{Provider: "UserSubtitle", Badge: "user"}, "MY", "action.stream.origin.my", nil},
		{ListItem{Provider: "Translated", Badge: "ai"}, "AI", "action.stream.origin.ai", nil},
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

// TestSubtitleLangGroupsExpandsPreferredOverTheActiveLanguage is the new
// tie-break on its own: the viewer's language has exactly one track and the
// playing one has three, so every other rule in the comparator (count,
// active, insertion order) points the other way. The row still opens on the
// preferred language, and the active one keeps its dot one slot over.
func TestSubtitleLangGroupsExpandsPreferredOverTheActiveLanguage(t *testing.T) {
	h := NewHelper()
	lis := []ListItem{
		{ID: "none", Label: "None"},
		li("a", "en", "MediaProbe", false),
		li("b", "en", "OpenSubtitles", true),
		li("c", "en", "ExportTag", false),
		li("d", "pt", "OpenSubtitles", false),
	}
	row := h.SubtitleLangGroups(lis, "pt")
	if row.Expanded != "pt" {
		t.Errorf("Expanded = %q, want pt", row.Expanded)
	}
	if row.Groups[0].Lang != "pt" || row.Groups[1].Lang != "en" {
		t.Fatalf("order = %+v, want pt then en", row.Groups)
	}
	if !row.Groups[1].Active {
		t.Error("the playing language keeps Active (the dot) where it sorts")
	}
}

// ...and an AI-only group counts: the preferred language whose only track
// is the translation the ladder added still opens the row. That is the
// whole point of the AI item — the viewer's language is there now.
func TestSubtitleLangGroupsExpandsPreferredWithOnlyATranslation(t *testing.T) {
	h := NewHelper()
	lis := []ListItem{
		{ID: "none", Label: "None"},
		li("a", "en", "MediaProbe", true),
		li("tr-pt", "pt", "Translated", false),
	}
	row := h.SubtitleLangGroups(lis, "pt")
	if row.Expanded != "pt" || row.Groups[0].Lang != "pt" {
		t.Fatalf("Expanded = %q, groups = %+v, want pt first", row.Expanded, row.Groups)
	}
}

// The preferred language with no tracks at all changes nothing: the row
// falls back to the language playing, exactly as before.
func TestSubtitleLangGroupsFallsBackToActiveWhenPreferredHasNoTracks(t *testing.T) {
	h := NewHelper()
	lis := []ListItem{
		{ID: "none", Label: "None"},
		li("a", "en", "MediaProbe", false),
		li("b", "en", "OpenSubtitles", false),
		li("c", "ru", "OpenSubtitles", true),
	}
	row := h.SubtitleLangGroups(lis, "pt")
	if row.Expanded != "ru" || row.Groups[0].Lang != "ru" {
		t.Fatalf("Expanded = %q, groups = %+v, want ru first", row.Expanded, row.Groups)
	}
}

// TestSubtitleLangGroupsExpandsTheSuggestedLanguage: with subtitles off and
// no group in the preferred language, "the biggest group" is an arbitrary
// answer — the row should open where the track the switch would turn on
// lives, so the viewer can see it. It also has to be first, not merely
// Expanded: a language past the sixth chip would be expanded and collapsed
// at once, a pressed filter with no visible chip.
func TestSubtitleLangGroupsExpandsTheSuggestedLanguage(t *testing.T) {
	h := NewHelper()
	lis := []ListItem{
		{ID: "none", Label: "None", Default: true},
		li("a", "en", "MediaProbe", false),
		li("b", "en", "OpenSubtitles", false),
		li("c", "en", "ExportTag", false),
		li("d", "ru", "UserSubtitle", false),
	}
	lis[4].Suggested = true

	row := h.SubtitleLangGroups(lis, "pt") // preferred language has no tracks
	if row.Expanded != "ru" {
		t.Errorf("Expanded = %q, want ru (the suggested track's language)", row.Expanded)
	}
	if row.Groups[0].Lang != "ru" {
		t.Fatalf("order = %+v, want ru first", row.Groups)
	}

	// The preferred language still wins when it has tracks of its own.
	lis = append(lis, li("e", "pt", "OpenSubtitles", false))
	if row := h.SubtitleLangGroups(lis, "pt"); row.Expanded != "pt" {
		t.Errorf("Expanded = %q, want pt: the preferred language still comes first", row.Expanded)
	}
}

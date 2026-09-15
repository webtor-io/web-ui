package action

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/text/language"

	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
)

func probeWith(streams string) *api.MediaProbe {
	var mp api.MediaProbe
	if err := json.Unmarshal([]byte(`{"streams":`+streams+`}`), &mp); err != nil {
		panic(err)
	}
	return &mp
}

func subtitleItems(items []ListItem) map[string]ListItem {
	m := map[string]ListItem{}
	for _, it := range items {
		if it.Provider == "MediaProbe" {
			m[it.ID] = it
		}
	}
	return m
}

func TestGetSubtitlesSkipsPGSWithoutIndexShift(t *testing.T) {
	// Transcoder drops hdmv_pgs from the HLS group, so the text track
	// that follows it is HLS subtitle #0, not #1.
	mp := probeWith(`[
		{"codec_type":"video","codec_name":"h264"},
		{"codec_type":"subtitle","codec_name":"hdmv_pgs_subtitle","tags":{"language":"eng"}},
		{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"rus","title":"Russian"}}
	]`)
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, mp, &ra.ExportTag{}, nil, &models.ExternalData{}, nil, SubtitleOpts{})
	got := subtitleItems(items)
	if len(got) != 1 {
		t.Fatalf("want exactly one embedded track, got %+v", got)
	}
	it, ok := got["mp-0"]
	if !ok || it.MPID != "0" || it.SrcLang != "ru" {
		t.Fatalf("got %+v", got)
	}
}

func TestGetSubtitlesHidesDVDSubButKeepsIndex(t *testing.T) {
	// dvd_subtitle IS in the transcoder's group (only PGS is excluded),
	// so it occupies HLS index 0 and the text track after it is #1.
	mp := probeWith(`[
		{"codec_type":"subtitle","codec_name":"dvd_subtitle","tags":{"language":"eng"}},
		{"codec_type":"subtitle","codec_name":"ass","tags":{"language":"eng"}}
	]`)
	got := subtitleItems(NewHelper().GetSubtitles(&models.VideoStreamUserData{}, mp, &ra.ExportTag{}, nil, &models.ExternalData{}, nil, SubtitleOpts{}))
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	if it, ok := got["mp-1"]; !ok || it.MPID != "1" {
		t.Fatalf("got %+v", got)
	}
}

// Forced tracks are listed (not hidden) and flagged with Forced/Badge, so
// the picker can show them with a "forced" badge instead of dropping them.
func TestGetSubtitlesHidesForcedByTitle(t *testing.T) {
	mp := probeWith(`[
		{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","title":"English (Forced)"}},
		{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","title":"English"}}
	]`)
	got := subtitleItems(NewHelper().GetSubtitles(&models.VideoStreamUserData{}, mp, &ra.ExportTag{}, nil, &models.ExternalData{}, nil, SubtitleOpts{}))
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if it, ok := got["mp-0"]; !ok || !it.Forced || it.Badge != "forced" {
		t.Fatalf("forced track must be listed and flagged: %+v", got)
	}
	if it, ok := got["mp-1"]; !ok || it.Forced || it.Label != "English" || it.Badge != "embedded" {
		t.Fatalf("got %+v", got)
	}
}

func TestGetSubtitlesCarriesOpenSubtitlesSource(t *testing.T) {
	os := []api.OpenSubtitleTrack{{ID: "7", Source: "imdb", ExportTrack: &ra.ExportTrack{Src: "u", SrcLang: "en", Label: "English", Kind: "subtitles"}}}
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, nil, &ra.ExportTag{}, os, &models.ExternalData{}, nil, SubtitleOpts{})
	for _, it := range items {
		if it.ID == "os-7" {
			if it.Source != "imdb" {
				t.Fatalf("got %+v", it)
			}
			return
		}
	}
	t.Fatal("os-7 not found")
}

func TestEmbeddedSubtitleVisible(t *testing.T) {
	cases := []struct {
		codec, title            string
		visible, counts, forced bool
	}{
		{"subrip", "", true, true, false},
		{"ass", "Signs & Songs", true, true, false},
		{"hdmv_pgs_subtitle", "", false, false, false},
		{"dvd_subtitle", "", false, true, false},
		{"dvb_subtitle", "", false, true, false},
		// Forced tracks are now visible and flagged, not hidden.
		{"subrip", "Forced", true, true, true},
		{"subrip", "eng forced narrative", true, true, true},
		{"subrip", "FORCED (English)", true, true, true},
		// "forced" as a substring of an ordinary word is not a forced
		// track -- hiding these loses a real subtitle stream.
		{"subrip", "Unforced", true, true, false},
		{"subrip", "Reinforced Steel", true, true, false},
	}
	for _, c := range cases {
		v, n, f := embeddedSubtitleVisible(c.codec, c.title)
		if v != c.visible || n != c.counts || f != c.forced {
			t.Errorf("%s/%q: got (%v,%v,%v) want (%v,%v,%v)", c.codec, c.title, v, n, f, c.visible, c.counts, c.forced)
		}
	}
}

// TestEmbeddedForcedIsVisibleAndFlagged pins the ladder change: a forced
// embedded track is listed (not dropped) and carries the "forced" badge,
// while its ordinary sibling keeps the plain "embedded" badge.
func TestEmbeddedForcedIsVisibleAndFlagged(t *testing.T) {
	mp := probeWith(`[
		{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","title":"English (Forced)"}},
		{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","title":"English"}}
	]`)
	got := subtitleItems(NewHelper().GetSubtitles(&models.VideoStreamUserData{}, mp, &ra.ExportTag{}, nil, &models.ExternalData{}, nil, SubtitleOpts{}))
	f, ok := got["mp-0"]
	if !ok || !f.Forced || f.Badge != "forced" {
		t.Fatalf("forced track must be listed and flagged: %+v", got)
	}
	if n, ok := got["mp-1"]; !ok || n.Forced || n.Badge != "embedded" {
		t.Fatalf("plain track: %+v", got)
	}
}

// TestSidecarForcedAndBadges pins the badge shown per provider, and that a
// sidecar track named/sourced "forced" gets the "forced" badge instead of
// "sidecar".
func TestSidecarForcedAndBadges(t *testing.T) {
	tag := &ra.ExportTag{Tracks: []ra.ExportTrack{
		{Src: "u1", SrcLang: "en", Label: "Movie.forced.srt", Kind: "subtitles"},
		{Src: "u2", SrcLang: "en", Label: "Movie.srt", Kind: "subtitles"},
	}}
	os := []api.OpenSubtitleTrack{osTrack("7", "en")}
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, nil, tag, os, &models.ExternalData{}, []models.UserSubtitleTrack{{ID: "us-1", Src: "u3", Label: "mine.srt", SrcLang: "en"}}, SubtitleOpts{})
	badges := map[string]ListItem{}
	for _, it := range items {
		badges[it.ID] = it
	}
	if !badges["et-1"].Forced || badges["et-1"].Badge != "forced" {
		t.Errorf("et-1: %+v", badges["et-1"])
	}
	if badges["et-2"].Badge != "sidecar" || badges["os-7"].Badge != "os" || badges["us-1"].Badge != "user" {
		t.Errorf("badges: et-2=%s os-7=%s us-1=%s", badges["et-2"].Badge, badges["os-7"].Badge, badges["us-1"].Badge)
	}
}

func osTrack(id, lang string) api.OpenSubtitleTrack {
	return api.OpenSubtitleTrack{ID: id, Source: "imdb", ExportTrack: &ra.ExportTrack{Src: "u" + id, SrcLang: lang, Label: lang + " " + id, Kind: "subtitles"}}
}

func TestGetSubtitlesPreloadsUILanguageAndEnglishOnly(t *testing.T) {
	var os []api.OpenSubtitleTrack
	for _, lang := range []string{"de", "pt", "en", "ru"} {
		for i := 1; i <= 3; i++ {
			os = append(os, osTrack(lang+strconv.Itoa(i), lang))
		}
	}
	ud := &models.VideoStreamUserData{AcceptLangTags: []language.Tag{language.BrazilianPortuguese}, FallbackLangTag: language.English}
	items := NewHelper().GetSubtitles(ud, nil, &ra.ExportTag{}, os, &models.ExternalData{}, nil, SubtitleOpts{})
	got := map[string]bool{}
	for _, it := range items {
		if it.Preload {
			got[it.ID] = true
		}
	}
	for _, id := range []string{"os-pt1", "os-pt2", "os-pt3", "os-en1", "os-en2", "os-en3"} {
		if !got[id] {
			t.Errorf("%s should be preloaded, got %v", id, got)
		}
	}
	for _, id := range []string{"os-de1", "os-ru1", "none"} {
		if got[id] {
			t.Errorf("%s must not be preloaded", id)
		}
	}
}

func TestGetSubtitlesPreloadIsCapped(t *testing.T) {
	var os []api.OpenSubtitleTrack
	for i := 1; i <= 12; i++ {
		os = append(os, osTrack("en"+strconv.Itoa(i), "en"))
	}
	ud := &models.VideoStreamUserData{AcceptLangTags: []language.Tag{language.English}, FallbackLangTag: language.English}
	n := 0
	for _, it := range NewHelper().GetSubtitles(ud, nil, &ra.ExportTag{}, os, &models.ExternalData{}, nil, SubtitleOpts{}) {
		if it.Preload {
			n++
		}
	}
	if n != maxPreloadTracks {
		t.Fatalf("preloaded %d tracks, want cap %d", n, maxPreloadTracks)
	}
}

func TestGetSubtitlesPreloadNeverIncludesEmbedded(t *testing.T) {
	mp := probeWith(`[{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng"}}]`)
	ud := &models.VideoStreamUserData{AcceptLangTags: []language.Tag{language.English}, FallbackLangTag: language.English}
	for _, it := range NewHelper().GetSubtitles(ud, mp, &ra.ExportTag{}, nil, &models.ExternalData{}, nil, SubtitleOpts{}) {
		if it.Provider == "MediaProbe" && it.Preload {
			t.Fatalf("embedded track marked preload: %+v", it)
		}
	}
}

// TestForcedTrackNeverAutoSelectedByLanguage pins the legacy
// language-matching path (used when there's no explicit selection and no
// SubtitleOpts.PreferredLang rule yet -- Task 3 adds that): a forced
// (signs-only) track must never win automatic selection just because it
// happens to match the viewer's language and precede the real track in
// probe order.
func TestForcedTrackNeverAutoSelectedByLanguage(t *testing.T) {
	mp := probeWith(`[
		{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","title":"English (Forced)"}},
		{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","title":"English"}}
	]`)
	ud := &models.VideoStreamUserData{AcceptLangTags: []language.Tag{language.English}, FallbackLangTag: language.English}
	got := subtitleItems(NewHelper().GetSubtitles(ud, mp, &ra.ExportTag{}, nil, &models.ExternalData{}, nil, SubtitleOpts{}))
	if it, ok := got["mp-0"]; !ok || it.Default {
		t.Fatalf("forced track must not be auto-selected: %+v", got)
	}
	if it, ok := got["mp-1"]; !ok || !it.Default {
		t.Fatalf("plain track in the accept language must be selected: %+v", got)
	}
}

// TestForcedOnlyLanguageMatchFallsThroughToNone covers the other half of the
// guard: when the ONLY track in the viewer's language is forced, that still
// must not become Default -- matchLang reports "no match" and
// selectListItem falls back to lis[0] ("None"), not to the forced track.
func TestForcedOnlyLanguageMatchFallsThroughToNone(t *testing.T) {
	mp := probeWith(`[
		{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","title":"English (Forced)"}},
		{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"rus","title":"Russian"}}
	]`)
	ud := &models.VideoStreamUserData{AcceptLangTags: []language.Tag{language.English}, FallbackLangTag: language.English}
	items := NewHelper().GetSubtitles(ud, mp, &ra.ExportTag{}, nil, &models.ExternalData{}, nil, SubtitleOpts{})
	got := subtitleItems(items)
	if it, ok := got["mp-0"]; !ok || it.Default {
		t.Fatalf("forced track must never be the language-match fallback: %+v", got)
	}
	found := false
	for _, it := range items {
		if it.ID == "none" {
			found = true
			if !it.Default {
				t.Fatalf("expected fallback to None when only a forced track matches the viewer's language: %+v", items)
			}
		}
	}
	if !found {
		t.Fatal("None item missing")
	}
}

// humanTracks is the common fixture of the ladder tests: one English
// sidecar plus two OpenSubtitles tracks (a German imdb match and an
// English hash match).
func humanTracks() (*ra.ExportTag, []api.OpenSubtitleTrack) {
	tag := &ra.ExportTag{Tracks: []ra.ExportTrack{{Src: "https://x/sc-en.vtt?token=T", SrcLang: "en", Label: "Movie.srt", Kind: "subtitles"}}}
	os := []api.OpenSubtitleTrack{
		{ID: "1", Source: "imdb", ExportTrack: &ra.ExportTrack{Src: "https://x/os-de.vtt?token=T", SrcLang: "de", Label: "German", Kind: "subtitles"}},
		{ID: "2", Source: "hash", ExportTrack: &ra.ExportTrack{Src: "https://x/os-en.vtt?token=T", SrcLang: "en", Label: "English", Kind: "subtitles"}},
	}
	return tag, os
}

func byID(items []ListItem) map[string]ListItem {
	m := map[string]ListItem{}
	for _, it := range items {
		m[it.ID] = it
	}
	return m
}

func defaultID(items []ListItem) string {
	for _, it := range items {
		if it.Default {
			return it.ID
		}
	}
	return ""
}

func audioProbe(lang string) *api.MediaProbe {
	return probeWith(`[{"codec_type":"audio","codec_name":"aac","tags":{"language":"` + lang + `"}},{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","title":"English"}}]`)
}

func TestLadderTranslatedIsDefaultWhenNoHumanTrackInPreferredLang(t *testing.T) {
	tag, os := humanTracks()
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, audioProbe("eng"), tag, os, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true, Names: []string{"Hildy"}})
	got := byID(items)
	tr, ok := got["tr-pt"]
	if !ok || tr.Provider != "Translated" || tr.SrcLang != "pt" || tr.Badge != "ai" || tr.Locked || tr.Preload {
		t.Fatalf("translated item: %+v", tr)
	}
	if !strings.Contains(tr.Src, "~tr:pt/") || !strings.Contains(tr.Src, "names=Hildy") {
		t.Fatalf("src=%q", tr.Src)
	}
	// source: audio is English → the English track wins (sidecar comes before OS in list order)
	if !strings.HasPrefix(tr.Src, "https://x/sc-en.vtt~tr:pt/") {
		t.Fatalf("expected the English sidecar as source, got %q", tr.Src)
	}
	if defaultID(items) != "tr-pt" {
		t.Fatalf("default=%s", defaultID(items))
	}
}

func TestLadderHumanTrackBeatsTranslation(t *testing.T) {
	tag, os := humanTracks()
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, audioProbe("eng"), tag, os, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "de", Translate: true, Paid: true})
	if defaultID(items) != "os-1" {
		t.Fatalf("default=%s want os-1 (human German)", defaultID(items))
	}
	if _, ok := byID(items)["tr-de"]; ok {
		t.Fatal("no AI item when a human track exists in the preferred language")
	}
}

func TestLadderOrderUserEmbeddedSidecarOS(t *testing.T) {
	mp := probeWith(`[{"codec_type":"audio","codec_name":"aac","tags":{"language":"eng"}},{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"rus","title":"Russian"}}]`)
	tag := &ra.ExportTag{Tracks: []ra.ExportTrack{{Src: "sc-ru", SrcLang: "ru", Label: "Movie.rus.srt", Kind: "subtitles"}}}
	os := []api.OpenSubtitleTrack{{ID: "9", Source: "hash", ExportTrack: &ra.ExportTrack{Src: "os-ru", SrcLang: "ru", Label: "Russian", Kind: "subtitles"}}}
	user := []models.UserSubtitleTrack{{ID: "us-1", Src: "u", Label: "mine.srt", SrcLang: "ru"}}
	opts := SubtitleOpts{PreferredLang: "ru", Translate: true, Paid: true}
	if d := defaultID(NewHelper().GetSubtitles(&models.VideoStreamUserData{}, mp, tag, os, &models.ExternalData{}, user, opts)); d != "us-1" {
		t.Errorf("user upload must win: %s", d)
	}
	if d := defaultID(NewHelper().GetSubtitles(&models.VideoStreamUserData{}, mp, tag, os, &models.ExternalData{}, nil, opts)); d != "mp-0" {
		t.Errorf("embedded must beat sidecar: %s", d)
	}
	if d := defaultID(NewHelper().GetSubtitles(&models.VideoStreamUserData{}, nil, tag, os, &models.ExternalData{}, nil, opts)); d != "et-1" {
		t.Errorf("sidecar must beat OpenSubtitles: %s", d)
	}
	if d := defaultID(NewHelper().GetSubtitles(&models.VideoStreamUserData{}, nil, &ra.ExportTag{}, os, &models.ExternalData{}, nil, opts)); d != "os-9" {
		t.Errorf("OpenSubtitles last: %s", d)
	}
}

func TestLadderOSHashBeatsImdb(t *testing.T) {
	os := []api.OpenSubtitleTrack{
		{ID: "1", Source: "imdb", ExportTrack: &ra.ExportTrack{Src: "a", SrcLang: "pt", Label: "pt", Kind: "subtitles"}},
		{ID: "2", Source: "hash", ExportTrack: &ra.ExportTrack{Src: "b", SrcLang: "pt", Label: "pt", Kind: "subtitles"}},
	}
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, audioProbe("eng"), &ra.ExportTag{}, os, &models.ExternalData{}, nil, SubtitleOpts{PreferredLang: "pt"})
	if defaultID(items) != "os-2" {
		t.Fatalf("default=%s want os-2 (hash match)", defaultID(items))
	}
}

func TestLadderAudioMatchesPreferredNoAutoSubtitles(t *testing.T) {
	tag, os := humanTracks()
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, audioProbe("por"), tag, os, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true})
	if defaultID(items) != "none" {
		t.Fatalf("default=%s want none", defaultID(items))
	}
	tr, ok := byID(items)["tr-pt"]
	if !ok || tr.Default {
		t.Fatalf("AI item must still be offered, not default: %+v", tr)
	}
}

func TestLadderAudioMatchesPreferredForcedIsDefault(t *testing.T) {
	mp := probeWith(`[{"codec_type":"audio","codec_name":"aac","tags":{"language":"por"}},
		{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"por","title":"Portuguese (Forced)"}},
		{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"por","title":"Portuguese"}}]`)
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, mp, &ra.ExportTag{}, nil, &models.ExternalData{}, nil, SubtitleOpts{PreferredLang: "pt"})
	if defaultID(items) != "mp-0" {
		t.Fatalf("default=%s want mp-0 (forced pt)", defaultID(items))
	}
}

func TestLadderForcedNeverFullDefaultNorSource(t *testing.T) {
	tag := &ra.ExportTag{Tracks: []ra.ExportTrack{{Src: "https://x/forced.vtt", SrcLang: "en", Label: "Movie.forced.srt", Kind: "subtitles"}}}
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, audioProbe("eng"), tag, nil, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true})
	if _, ok := byID(items)["tr-pt"]; ok {
		t.Fatal("a forced track is not a translation source")
	}
	if d := defaultID(items); d == "et-1" {
		t.Fatal("forced must not be the full default")
	}
}

// TestLadderForcedInPreferredLangIsNotTheDefault is the negative control of
// the Forced guard in bestByLadder: a forced track that IS in the preferred
// language, with the audio in another one, still must not be selected —
// only the audio rule (TestLadderAudioMatchesPreferredForcedIsDefault) may
// turn a forced track on.
func TestLadderForcedInPreferredLangIsNotTheDefault(t *testing.T) {
	tag := &ra.ExportTag{Tracks: []ra.ExportTrack{{Src: "https://x/pt.vtt", SrcLang: "pt", Label: "Movie.forced.srt", Kind: "subtitles"}}}
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, audioProbe("eng"), tag, nil, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true})
	if d := defaultID(items); d != "none" {
		t.Fatalf("default=%s: a forced track must not be the full-subtitle default", d)
	}
}

// TestLadderLockedForFree: a free viewer sees the AI item with a lock and
// no Src, and it is never the default -- a "selected" track that cannot be
// turned on would leave the player with subtitles on and nothing on
// screen. The default comes from the phase-1 fallback instead.
func TestLadderLockedForFree(t *testing.T) {
	tag, os := humanTracks()
	ud := &models.VideoStreamUserData{FallbackLangTag: language.English}
	items := NewHelper().GetSubtitles(ud, audioProbe("eng"), tag, os, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: false})
	tr := byID(items)["tr-pt"]
	if !tr.Locked || tr.Src != "" || tr.Default {
		t.Fatalf("free: %+v", tr)
	}
	if d := defaultID(items); d != "mp-0" {
		t.Fatalf("default=%s: want the Accept-Language pick (the embedded English track)", d)
	}
}

func TestLadderSavedChoiceWins(t *testing.T) {
	tag, os := humanTracks()
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{SubtitleID: "os-1"}, audioProbe("eng"), tag, os, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true})
	if defaultID(items) != "os-1" {
		t.Fatalf("saved choice must win: %s", defaultID(items))
	}
}

// TestLadderSavedChoiceIsMarkedSaved pins the difference between "the
// viewer chose this" and "the ladder picked this". The player re-runs the
// ladder when the audio track changes, and Default alone cannot tell it
// which of the two it is looking at: without Saved it would re-decide
// over a choice the viewer made explicitly and switch their subtitles
// off.
func TestLadderSavedChoiceIsMarkedSaved(t *testing.T) {
	tag, os := humanTracks()
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{SubtitleID: "os-1"}, audioProbe("eng"), tag, os, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true})
	for _, li := range items {
		if li.ID == "os-1" {
			if !li.Saved {
				t.Fatal("the saved choice must be marked Saved")
			}
			continue
		}
		if li.Saved {
			t.Fatalf("only the saved choice may be marked Saved, got %s", li.ID)
		}
	}
}

// TestLadderPickIsNotMarkedSaved is the negative half: an item the ladder
// chose is Default but not Saved, so the audio rule stays free to
// override it.
func TestLadderPickIsNotMarkedSaved(t *testing.T) {
	tag, os := humanTracks()
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, audioProbe("eng"), tag, os, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true})
	for _, li := range items {
		if li.Saved {
			t.Fatalf("no saved choice was made, but %s is marked Saved", li.ID)
		}
	}
}

// TestLegacySavedChoiceIsMarkedSaved covers the phase-1 path (no
// preferred language): the same saved id goes through selectListItem.
func TestLegacySavedChoiceIsMarkedSaved(t *testing.T) {
	tag, os := humanTracks()
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{SubtitleID: "os-1", FallbackLangTag: language.English}, audioProbe("eng"), tag, os, &models.ExternalData{}, nil,
		SubtitleOpts{})
	saved := byID(items)["os-1"]
	if !saved.Default || !saved.Saved {
		t.Fatalf("legacy saved choice: default=%v saved=%v", saved.Default, saved.Saved)
	}
	for _, li := range items {
		if li.ID != "os-1" && li.Saved {
			t.Fatalf("only the saved choice may be marked Saved, got %s", li.ID)
		}
	}
}

func TestLadderSourcePrefersAudioLangThenEnglish(t *testing.T) {
	tag := &ra.ExportTag{Tracks: []ra.ExportTrack{
		{Src: "https://x/sc-en.vtt", SrcLang: "en", Label: "en.srt", Kind: "subtitles"},
		{Src: "https://x/sc-fr.vtt", SrcLang: "fr", Label: "fr.srt", Kind: "subtitles"},
	}}
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, audioProbe("fra"), tag, nil, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true})
	if tr := byID(items)["tr-pt"]; !strings.HasPrefix(tr.Src, "https://x/sc-fr.vtt~tr:pt/") {
		t.Fatalf("French audio → French source, got %q", tr.Src)
	}
	items = NewHelper().GetSubtitles(&models.VideoStreamUserData{}, audioProbe("deu"), tag, nil, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true})
	if tr := byID(items)["tr-pt"]; !strings.HasPrefix(tr.Src, "https://x/sc-en.vtt~tr:pt/") {
		t.Fatalf("no source in the audio language → English, got %q", tr.Src)
	}
}

func TestLadderNoTranslationWithoutSource(t *testing.T) {
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, audioProbe("eng"), &ra.ExportTag{}, nil, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true})
	if _, ok := byID(items)["tr-pt"]; ok {
		t.Fatal("embedded-only files have no translation source in phase 2")
	}
}

func TestLadderDisabledFallsBackToOldSelection(t *testing.T) {
	tag, os := humanTracks()
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{AcceptLangTags: []language.Tag{language.German}, FallbackLangTag: language.English}, nil, tag, os, &models.ExternalData{}, nil, SubtitleOpts{})
	if defaultID(items) != "os-1" {
		t.Fatalf("without PreferredLang the Accept-Language match applies: %s", defaultID(items))
	}
}

// TestLadderRankAndSourceBadge pins the two fields the template and the
// player read instead of reimplementing the ladder (R-N, R-O): every item
// carries its ladder rank, and the AI item names the origin of the track
// it was translated from.
func TestLadderRankAndSourceBadge(t *testing.T) {
	tag, os := humanTracks()
	user := []models.UserSubtitleTrack{{ID: "us-1", Src: "https://x/mine.vtt", Label: "mine.srt", SrcLang: "ja"}}
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, audioProbe("eng"), tag, os, &models.ExternalData{}, user,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true})
	got := byID(items)
	for id, want := range map[string]int{"us-1": 0, "mp-0": 1, "et-1": 2, "os-2": 3, "os-1": 4, "tr-pt": 5} {
		it, ok := got[id]
		if !ok {
			t.Fatalf("%s missing from %+v", id, got)
		}
		if it.Rank != want {
			t.Errorf("%s rank=%d want %d", id, it.Rank, want)
		}
	}
	tr := got["tr-pt"]
	if tr.SourceID != "et-1" || tr.SourceBadge != "sidecar" {
		t.Errorf("AI item must name its source: sourceID=%q badge=%q", tr.SourceID, tr.SourceBadge)
	}
	if tr.Source != "" {
		t.Errorf("Source is the OpenSubtitles hash|imdb enum and goes to telemetry; the AI item must leave it empty, got %q", tr.Source)
	}
}

// TestLadderExternalDefaultIsKept covers an embed that asked for a specific
// track: that explicit choice stands, and the ladder must not add a second
// Default item to the list.
func TestLadderExternalDefaultIsKept(t *testing.T) {
	ext := &models.ExternalData{Tracks: []models.ExternalTrack{{Src: "https://x/e.vtt", SrcLang: "en", Label: "External", Default: true}}}
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, audioProbe("eng"), &ra.ExportTag{}, nil, ext, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true})
	n := 0
	for _, it := range items {
		if it.Default {
			n++
		}
	}
	if n != 1 || defaultID(items) != "ext-1" {
		t.Fatalf("want exactly one default (ext-1), got %d, default=%s", n, defaultID(items))
	}
}

// TestLadderNoPreferredMatchKeepsPhaseOneSelection: the preferred language
// yields neither a human track nor an AI one (translation off). The ladder
// must not switch subtitles off -- the phase-1 Accept-Language selection
// still decides.
func TestLadderNoPreferredMatchKeepsPhaseOneSelection(t *testing.T) {
	mp := probeWith(`[{"codec_type":"audio","codec_name":"aac","tags":{"language":"jpn"}}]`)
	os := []api.OpenSubtitleTrack{osTrack("7", "en")}
	ud := &models.VideoStreamUserData{FallbackLangTag: language.English}
	items := NewHelper().GetSubtitles(ud, mp, &ra.ExportTag{}, os, &models.ExternalData{}, nil, SubtitleOpts{PreferredLang: "pt"})
	if d := defaultID(items); d != "os-7" {
		t.Fatalf("default=%s want os-7: a ladder miss must not take subtitles away", d)
	}
}

// TestLadderUnknownLanguageHasNoAIItem: a preferred language the
// translation service does not know gets no AI item at all -- an item that
// leads to a rejected request is worse than no item.
func TestLadderUnknownLanguageHasNoAIItem(t *testing.T) {
	tag, os := humanTracks()
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{FallbackLangTag: language.English}, audioProbe("eng"), tag, os, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "eo", Translate: true, Paid: true})
	if _, ok := byID(items)["tr-eo"]; ok {
		t.Fatal("no AI item for a language the service does not support")
	}
}

// TestLadderSavedChoiceClearsExternalDefault: an embed marks a track
// Default, the viewer has since picked another one. Exactly one item may
// end up Default, and it is the viewer's.
func TestLadderSavedChoiceClearsExternalDefault(t *testing.T) {
	ext := &models.ExternalData{Tracks: []models.ExternalTrack{{Src: "https://x/e.vtt", SrcLang: "en", Label: "External", Default: true}}}
	os := []api.OpenSubtitleTrack{osTrack("7", "de")}
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{SubtitleID: "os-7"}, audioProbe("eng"), &ra.ExportTag{}, os, ext, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true})
	n := 0
	for _, it := range items {
		if it.Default {
			n++
		}
	}
	if n != 1 || defaultID(items) != "os-7" {
		t.Fatalf("want exactly one default (os-7), got %d, default=%s", n, defaultID(items))
	}
}

// TestLadderLockedNeverWinsLanguageFallback: the ladder-miss fallback runs
// the legacy Accept-Language match, which must not see AI items at all --
// otherwise the locked tr-pt item is the only "pt" track in the list and
// wins the match with an empty Src.
func TestLadderLockedNeverWinsLanguageFallback(t *testing.T) {
	tag, os := humanTracks()
	ud := &models.VideoStreamUserData{AcceptLangTags: []language.Tag{language.Portuguese}, FallbackLangTag: language.English}
	items := NewHelper().GetSubtitles(ud, audioProbe("eng"), tag, os, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: false})
	tr, ok := byID(items)["tr-pt"]
	if !ok || !tr.Locked {
		t.Fatalf("the locked AI item must still be listed: %+v", tr)
	}
	if d := defaultID(items); d == "tr-pt" {
		t.Fatal("a locked item must never be the Accept-Language match")
	}
}

// TestLadderLockedNeverWinsEnglishFallback is the same hole reached through
// the fallback language rather than Accept-Language: FallbackLangTag is
// always English, so a locked tr-en item would win whenever the file has no
// English human track.
func TestLadderLockedNeverWinsEnglishFallback(t *testing.T) {
	tag := &ra.ExportTag{Tracks: []ra.ExportTrack{{Src: "https://x/sc-de.vtt", SrcLang: "de", Label: "German.srt", Kind: "subtitles"}}}
	mp := probeWith(`[{"codec_type":"audio","codec_name":"aac","tags":{"language":"jpn"}}]`)
	ud := &models.VideoStreamUserData{FallbackLangTag: language.English}
	items := NewHelper().GetSubtitles(ud, mp, tag, nil, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "en", Translate: true, Paid: false})
	tr, ok := byID(items)["tr-en"]
	if !ok || !tr.Locked {
		t.Fatalf("the locked AI item must still be listed: %+v", tr)
	}
	if d := defaultID(items); d == "tr-en" {
		t.Fatalf("a locked item must never be the English fallback, default=%s", d)
	}
}

// TestLadderSavedLockedChoiceIgnored: the viewer saved the AI track while
// they were paying and has since dropped to free. The stale choice must not
// select a track they cannot turn on.
func TestLadderSavedLockedChoiceIgnored(t *testing.T) {
	tag, os := humanTracks()
	ud := &models.VideoStreamUserData{SubtitleID: "tr-pt", FallbackLangTag: language.English}
	items := NewHelper().GetSubtitles(ud, audioProbe("eng"), tag, os, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: false})
	if d := defaultID(items); d == "tr-pt" {
		t.Fatal("a saved choice pointing at a locked item must be ignored")
	}
	if tr := byID(items)["tr-pt"]; !tr.Locked || tr.Src != "" {
		t.Fatalf("the item stays listed and locked: %+v", tr)
	}
}

// TestLadderSavedAIChoiceHonouredWhenPaid is the other half: the same saved
// choice is still honoured for a viewer who may activate it.
func TestLadderSavedAIChoiceHonouredWhenPaid(t *testing.T) {
	tag, os := humanTracks()
	ud := &models.VideoStreamUserData{SubtitleID: "tr-pt", FallbackLangTag: language.English}
	items := NewHelper().GetSubtitles(ud, audioProbe("eng"), tag, os, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true})
	if d := defaultID(items); d != "tr-pt" {
		t.Fatalf("default=%s: the saved AI choice must win for a paid viewer", d)
	}
}

// TestUserSubtitleViewCarriesDefaultAndSaved pins the seam between the
// ladder and the "My Subtitles" tab. That tab renders from its own view
// model, so the items it produces never carried Default/Saved — a viewer
// whose saved choice is one of their own uploads got a list where nothing
// is marked, and the player's audio-switch rule (which reads data-saved off
// the DOM) could not see the choice at all and re-decided over it.
func TestUserSubtitleViewCarriesDefaultAndSaved(t *testing.T) {
	h := NewHelper()
	userSubs := []models.UserSubtitleTrack{
		{ID: "us-1", Label: "a.srt", Src: "https://x/a.vtt"},
		{ID: "us-2", Label: "b.srt", Src: "https://x/b.vtt"},
	}
	items := h.GetSubtitles(&models.VideoStreamUserData{SubtitleID: "us-2"}, audioProbe("eng"), &ra.ExportTag{}, nil, &models.ExternalData{}, userSubs,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true})
	if defaultID(items) != "us-2" {
		t.Fatalf("fixture: the saved upload must be the default, got %s", defaultID(items))
	}

	v := h.UserSubtitleView("res", "/movie.mkv", "http://ei", userSubs, items, "pt")
	if len(v.UserSubtitles) != 2 {
		t.Fatalf("view must keep every upload: %+v", v.UserSubtitles)
	}
	if v.UserSubtitles[0].Default || v.UserSubtitles[0].Saved {
		t.Fatalf("us-1 is neither the default nor the saved choice: %+v", v.UserSubtitles[0])
	}
	if !v.UserSubtitles[1].Default || !v.UserSubtitles[1].Saved {
		t.Fatalf("us-2 is the viewer's saved default: %+v", v.UserSubtitles[1])
	}
}

// TestUserSubtitleViewLadderPickIsNotSaved is the negative half: an upload
// the ladder picked is Default but not Saved, so the audio rule may still
// override it.
func TestUserSubtitleViewLadderPickIsNotSaved(t *testing.T) {
	h := NewHelper()
	userSubs := []models.UserSubtitleTrack{{ID: "us-1", Label: "a.pt.srt", SrcLang: "pt", Src: "https://x/a.vtt"}}
	items := h.GetSubtitles(&models.VideoStreamUserData{}, audioProbe("eng"), &ra.ExportTag{}, nil, &models.ExternalData{}, userSubs,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true})
	if defaultID(items) != "us-1" {
		t.Fatalf("fixture: the ladder must pick the Portuguese upload, got %s", defaultID(items))
	}
	v := h.UserSubtitleView("res", "/movie.mkv", "http://ei", userSubs, items, "pt")
	if !v.UserSubtitles[0].Default || v.UserSubtitles[0].Saved {
		t.Fatalf("a ladder pick is Default but not Saved: %+v", v.UserSubtitles[0])
	}
}

// TestAudioTracksAreNeverMarkedSaved pins that Saved is a subtitle-only
// field. The audio list shares selectListItem with the subtitle list, so a
// saved audio choice used to set Saved on an audio item — a value nothing
// reads (the template renders data-saved on subtitle items only) and that
// would mean something different if it ever were read: for subtitles Saved
// switches the audio-language rule off, and the audio list has no such rule.
func TestAudioTracksAreNeverMarkedSaved(t *testing.T) {
	ud := &models.VideoStreamUserData{AudioID: "mp-1", FallbackLangTag: language.English}
	mp := probeWith(`[
		{"codec_type":"audio","codec_name":"aac","tags":{"language":"eng"}},
		{"codec_type":"audio","codec_name":"aac","tags":{"language":"por"}}
	]`)
	tracks := NewHelper().GetAudioTracks(ud, mp)
	if len(tracks) != 2 {
		t.Fatalf("fixture: expected 2 audio tracks, got %d", len(tracks))
	}
	if defaultID(tracks) != "mp-1" {
		t.Fatalf("fixture: the saved audio choice must be the default, got %s", defaultID(tracks))
	}
	for _, a := range tracks {
		if a.Saved {
			t.Errorf("audio item %s is marked Saved; Saved is a subtitle-only field", a.ID)
		}
	}
}

// TestLadderNoTranslatedItemWithoutAUsableURL covers the pairing between
// TranslateURL's absolute-URL guard and the ladder: when the source track's
// Src is not something the proxy can be pointed at, TranslateURL returns ""
// and a paid viewer must get no item rather than one that selects and shows
// nothing. A free viewer is unaffected — the locked item never had a Src.
func TestLadderNoTranslatedItemWithoutAUsableURL(t *testing.T) {
	relative := &ra.ExportTag{Tracks: []ra.ExportTrack{
		{Src: "/ext/abc/movie.srt~vtt/movie.vtt", SrcLang: "en", Label: "Movie.srt", Kind: "subtitles"},
	}}
	paid := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, audioProbe("eng"), relative, nil, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true})
	if _, ok := byID(paid)["tr-pt"]; ok {
		t.Fatalf("a paid viewer must not get an AI item with no URL behind it: %+v", byID(paid)["tr-pt"])
	}

	// Negative control on the fixture: the same source with an absolute
	// URL does produce the item, so the test is not passing for the wrong
	// reason (say, pickTranslationSource rejecting the track outright).
	absolute := &ra.ExportTag{Tracks: []ra.ExportTrack{
		{Src: "https://x.test/ext/abc/movie.srt~vtt/movie.vtt", SrcLang: "en", Label: "Movie.srt", Kind: "subtitles"},
	}}
	ok2 := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, audioProbe("eng"), absolute, nil, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true})
	tr, found := byID(ok2)["tr-pt"]
	if !found || tr.Src == "" {
		t.Fatalf("an absolute source still produces the AI item: %+v", tr)
	}

	// A free viewer gets the locked upsell either way: it never carries a
	// Src, so there is no dead URL to protect them from.
	free := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, audioProbe("eng"), relative, nil, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: false})
	if lock, found := byID(free)["tr-pt"]; !found || !lock.Locked || lock.Src != "" {
		t.Fatalf("free viewer keeps the locked item: %+v", lock)
	}
}

// suggestedID is the item the picker would turn on when the viewer flips
// subtitles back on. Exactly one item may carry it.
func suggestedID(t *testing.T, items []ListItem) string {
	t.Helper()
	id := ""
	for _, it := range items {
		if !it.Suggested {
			continue
		}
		if id != "" {
			t.Fatalf("more than one item is Suggested: %s and %s", id, it.ID)
		}
		id = it.ID
	}
	return id
}

// TestLadderSavedOffSuggestsWhatItWouldHavePicked: "subtitles off" is a
// state of the picker's toggle, not the absence of a choice. The ladder
// still has to say what the toggle would turn on, or switching it on would
// have nothing to activate on a page the viewer opened with subtitles off.
func TestLadderSavedOffSuggestsWhatItWouldHavePicked(t *testing.T) {
	tag, os := humanTracks()
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{SubtitleID: "none"}, audioProbe("eng"), tag, os, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "de", Translate: true, Paid: true})
	if d := defaultID(items); d != "none" {
		t.Fatalf("default=%s want none: the saved off must stand", d)
	}
	if !byID(items)["none"].Saved {
		t.Fatal("the saved off must be marked Saved")
	}
	// The same item TestLadderHumanTrackBeatsTranslation gets as Default
	// when nothing is saved.
	if s := suggestedID(t, items); s != "os-1" {
		t.Fatalf("suggested=%s want os-1 (the human German track the ladder would pick)", s)
	}
}

// TestLadderSavedOffSuggestsTheAIItem is the same rule one rung down the
// ladder: no human track in the preferred language, so what the toggle
// would turn on is the AI translation.
func TestLadderSavedOffSuggestsTheAIItem(t *testing.T) {
	tag, os := humanTracks()
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{SubtitleID: "none"}, audioProbe("eng"), tag, os, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true})
	if s := suggestedID(t, items); s != "tr-pt" {
		t.Fatalf("suggested=%s want tr-pt", s)
	}
}

// TestLadderSavedOffNeverSuggestsALockedItem: a free viewer cannot
// activate the AI track, so it must not be what the toggle promises. The
// phase-1 fallback decides instead.
func TestLadderSavedOffNeverSuggestsALockedItem(t *testing.T) {
	tag, os := humanTracks()
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{SubtitleID: "none", FallbackLangTag: language.English}, audioProbe("eng"), tag, os, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: false})
	s := suggestedID(t, items)
	if s == "tr-pt" {
		t.Fatal("a locked item cannot be suggested: turning subtitles on would show nothing")
	}
	// mp-0 is the embedded English track audioProbe ships: first in list
	// order, so it is the Accept-Language match.
	if s != "mp-0" {
		t.Fatalf("suggested=%s want mp-0 (the phase-1 Accept-Language pick)", s)
	}
}

// TestLegacySavedOffSuggestsThePhaseOnePick covers the path with no
// preferred content language: the suggestion is what the Accept-Language
// selection would have chosen.
func TestLegacySavedOffSuggestsThePhaseOnePick(t *testing.T) {
	tag, os := humanTracks()
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{SubtitleID: "none", FallbackLangTag: language.English}, audioProbe("eng"), tag, os, &models.ExternalData{}, nil,
		SubtitleOpts{})
	if d := defaultID(items); d != "none" {
		t.Fatalf("default=%s want none", d)
	}
	if s := suggestedID(t, items); s != "mp-0" {
		t.Fatalf("suggested=%s want mp-0 (the embedded English track, first in list order)", s)
	}
}

// TestLadderSavedOffSuggestsTheEmbedsOwnTrack: an embed that asked for a
// specific track keeps that answer even as a suggestion -- the ladder is
// consulted only where the caller said nothing.
func TestLadderSavedOffSuggestsTheEmbedsOwnTrack(t *testing.T) {
	ext := &models.ExternalData{Tracks: []models.ExternalTrack{{Src: "https://x/e.vtt", SrcLang: "en", Label: "External", Default: true}}}
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{SubtitleID: "none"}, audioProbe("eng"), &ra.ExportTag{}, nil, ext, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true})
	if d := defaultID(items); d != "none" {
		t.Fatalf("default=%s want none", d)
	}
	if s := suggestedID(t, items); s != "ext-1" {
		t.Fatalf("suggested=%s want ext-1", s)
	}
}

// TestSuggestedOnlyExistsWhenSubtitlesAreOff is the negative control for
// the guard: with any other saved choice -- or none at all -- there is
// nothing to restore, and a Suggested item next to a Default one would
// give the picker two answers to the same question.
func TestSuggestedOnlyExistsWhenSubtitlesAreOff(t *testing.T) {
	tag, os := humanTracks()
	for _, saved := range []string{"", "os-1"} {
		items := NewHelper().GetSubtitles(&models.VideoStreamUserData{SubtitleID: saved, FallbackLangTag: language.English}, audioProbe("eng"), tag, os, &models.ExternalData{}, nil,
			SubtitleOpts{PreferredLang: "de", Translate: true, Paid: true})
		if s := suggestedID(t, items); s != "" {
			t.Fatalf("saved=%q: nothing may be Suggested while subtitles are on, got %s", saved, s)
		}
		// ...and the legacy path says the same.
		items = NewHelper().GetSubtitles(&models.VideoStreamUserData{SubtitleID: saved, FallbackLangTag: language.English}, audioProbe("eng"), tag, os, &models.ExternalData{}, nil,
			SubtitleOpts{})
		if s := suggestedID(t, items); s != "" {
			t.Fatalf("saved=%q (legacy): nothing may be Suggested while subtitles are on, got %s", saved, s)
		}
	}
}

// TestOffByTheLadderAlsoSuggestsATrack: the ladder reaches "no subtitles"
// on its own whenever the audio is already in the viewer's language --
// which is the common case, and the one where the switch would otherwise
// have nothing to turn on. It suggests the best track in that language.
func TestOffByTheLadderAlsoSuggestsATrack(t *testing.T) {
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, audioProbe("eng"), &ra.ExportTag{}, nil, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "en", Translate: true, Paid: true})
	if d := defaultID(items); d != "none" {
		t.Fatalf("default=%s want none (English audio, English viewer)", d)
	}
	if s := suggestedID(t, items); s != "mp-0" {
		t.Fatalf("suggested=%s want mp-0 (the embedded English track)", s)
	}
}

// TestNothingToTurnOnSuggestsNothing is the other half: a list whose only
// subtitle is a locked AI item has nothing the switch can give, and
// promising the item that means "off" -- or one that shows nothing -- would
// be a lie.
func TestNothingToTurnOnSuggestsNothing(t *testing.T) {
	tag := &ra.ExportTag{Tracks: []ra.ExportTrack{{Src: "https://x/sc-en.vtt", SrcLang: "en", Label: "en.srt", Kind: "subtitles"}}}
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{SubtitleID: "none"}, audioProbe("eng"), tag, nil, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: false})
	locked := byID(items)["tr-pt"]
	if !locked.Locked {
		t.Fatalf("fixture no longer produces a locked AI item: %+v", locked)
	}
	for _, it := range items {
		if it.Suggested && (it.Locked || it.ID == "none") {
			t.Fatalf("the switch must not promise %s (locked=%v)", it.ID, it.Locked)
		}
	}

	// ...and with no subtitle at all on the list, nothing is suggested.
	empty := NewHelper().GetSubtitles(&models.VideoStreamUserData{SubtitleID: "none"}, probeWith(`[{"codec_type":"audio","codec_name":"aac","tags":{"language":"eng"}}]`), &ra.ExportTag{}, nil, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true})
	if s := suggestedID(t, empty); s != "" {
		t.Fatalf("suggested=%s want nothing: there is no subtitle to turn on", s)
	}
}

// TestSavedOffAndLadderOffSuggestTheSameTrack is an outcome test: the
// suggestion is the ladder's own answer in both directions. With the audio
// already in the viewer's language and a forced track available, the ladder
// turns on the forced track — so that is what the switch promises, whether
// the viewer saved "off" themselves or the ladder arrived there.
func TestSavedOffAndLadderOffSuggestTheSameTrack(t *testing.T) {
	// English audio, English preference, one full English track and one
	// forced English track.
	mp := probeWith(`[
		{"codec_type":"audio","codec_name":"aac","tags":{"language":"eng"}},
		{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","title":"English"}},
		{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","title":"Forced (English)"}}
	]`)
	opts := SubtitleOpts{PreferredLang: "en", Translate: true, Paid: true}

	// The ladder's own answer, nothing saved: the forced track plays.
	on := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, mp, &ra.ExportTag{}, nil, &models.ExternalData{}, nil, opts)
	if d := defaultID(on); d != "mp-1" {
		t.Fatalf("default=%s want mp-1 (the forced track) -- fixture no longer covers this case", d)
	}

	// The viewer saved "off" instead. The switch must promise the same
	// track, not the full one a different order would have picked.
	off := NewHelper().GetSubtitles(&models.VideoStreamUserData{SubtitleID: "none"}, mp, &ra.ExportTag{}, nil, &models.ExternalData{}, nil, opts)
	if s := suggestedID(t, off); s != "mp-1" {
		t.Fatalf("suggested=%s want mp-1: saved-off and the ladder must agree", s)
	}
}

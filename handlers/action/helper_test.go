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

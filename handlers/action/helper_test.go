package action

import (
	"encoding/json"
	"strconv"
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

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
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, mp, &ra.ExportTag{}, nil, &models.ExternalData{}, nil)
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
	got := subtitleItems(NewHelper().GetSubtitles(&models.VideoStreamUserData{}, mp, &ra.ExportTag{}, nil, &models.ExternalData{}, nil))
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	if it, ok := got["mp-1"]; !ok || it.MPID != "1" {
		t.Fatalf("got %+v", got)
	}
}

func TestGetSubtitlesHidesForcedByTitle(t *testing.T) {
	mp := probeWith(`[
		{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","title":"English (Forced)"}},
		{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","title":"English"}}
	]`)
	got := subtitleItems(NewHelper().GetSubtitles(&models.VideoStreamUserData{}, mp, &ra.ExportTag{}, nil, &models.ExternalData{}, nil))
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	if it, ok := got["mp-1"]; !ok || it.Label != "English" {
		t.Fatalf("got %+v", got)
	}
}

func TestGetSubtitlesCarriesOpenSubtitlesSource(t *testing.T) {
	os := []api.OpenSubtitleTrack{{ID: "7", Source: "imdb", ExportTrack: &ra.ExportTrack{Src: "u", SrcLang: "en", Label: "English", Kind: "subtitles"}}}
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, nil, &ra.ExportTag{}, os, &models.ExternalData{}, nil)
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
		codec, title    string
		visible, counts bool
	}{
		{"subrip", "", true, true},
		{"ass", "Signs & Songs", true, true},
		{"hdmv_pgs_subtitle", "", false, false},
		{"dvd_subtitle", "", false, true},
		{"dvb_subtitle", "", false, true},
		{"subrip", "Forced", false, true},
		{"subrip", "eng forced narrative", false, true},
		{"subrip", "FORCED (English)", false, true},
		// "forced" as a substring of an ordinary word is not a forced
		// track -- hiding these loses a real subtitle stream.
		{"subrip", "Unforced", true, true},
		{"subrip", "Reinforced Steel", true, true},
	}
	for _, c := range cases {
		v, n := embeddedSubtitleVisible(c.codec, c.title)
		if v != c.visible || n != c.counts {
			t.Errorf("%s/%q: got (%v,%v) want (%v,%v)", c.codec, c.title, v, n, c.visible, c.counts)
		}
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
	items := NewHelper().GetSubtitles(ud, nil, &ra.ExportTag{}, os, &models.ExternalData{}, nil)
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
	for _, it := range NewHelper().GetSubtitles(ud, nil, &ra.ExportTag{}, os, &models.ExternalData{}, nil) {
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
	for _, it := range NewHelper().GetSubtitles(ud, mp, &ra.ExportTag{}, nil, &models.ExternalData{}, nil) {
		if it.Provider == "MediaProbe" && it.Preload {
			t.Fatalf("embedded track marked preload: %+v", it)
		}
	}
}

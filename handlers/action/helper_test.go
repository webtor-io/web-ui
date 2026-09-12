package action

import (
	"encoding/json"
	"testing"

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

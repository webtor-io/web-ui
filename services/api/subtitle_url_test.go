package api

import (
	"strings"
	"testing"

	ra "github.com/webtor-io/rest-api/services"
)

func TestAttachExternalSubtitleConvertsTextFormats(t *testing.T) {
	ei := ra.ExportItem{URL: "https://edge.example.com/hash/file.mp4?token=abc"}
	for _, tc := range []struct {
		name        string
		u           string
		wantConvert bool
		wantName    string
	}{
		{name: "srt", u: "https://webtor.io/user-subtitle/file/deadbeef/subs.srt", wantConvert: true, wantName: "subs.vtt"},
		{name: "ass", u: "https://webtor.io/user-subtitle/file/deadbeef/subs.ass", wantConvert: true, wantName: "subs.vtt"},
		{name: "ssa", u: "https://webtor.io/user-subtitle/file/deadbeef/subs.ssa", wantConvert: true, wantName: "subs.vtt"},
		{name: "vtt passes through", u: "https://webtor.io/user-subtitle/file/deadbeef/subs.vtt", wantConvert: false},
		// ".srt" уже входит в имя ресурса, а не является расширением.
		{name: "no extension", u: "https://webtor.io/user-subtitle/file/deadbeef/subtitles", wantConvert: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := (&Api{}).AttachExternalSubtitle(ei, tc.u)
			gotConvert := strings.Contains(res, "~vtt/")
			if gotConvert != tc.wantConvert {
				t.Fatalf("~vtt conversion = %v, want %v; url: %s", gotConvert, tc.wantConvert, res)
			}
			if tc.wantConvert && !strings.HasSuffix(strings.Split(res, "?")[0], "/"+tc.wantName) {
				t.Errorf("converted url does not end with %q: %s", tc.wantName, res)
			}
			if !strings.Contains(res, "/ext/") {
				t.Errorf("url is not wrapped through /ext/: %s", res)
			}
			if !strings.HasSuffix(res, "?token=abc") {
				t.Errorf("export-item query lost: %s", res)
			}
		})
	}
}

func TestMakeSubtitleURLConvertsTextFormats(t *testing.T) {
	base := "https://edge.example.com/hash/file.mp4~vi/opensubtitles?token=abc"
	for _, tc := range []struct {
		format      string
		src         string
		wantConvert bool
	}{
		{format: "srt", src: "/one.srt", wantConvert: true},
		{format: "ass", src: "/two.ass", wantConvert: true},
		{format: "ssa", src: "/three.ssa", wantConvert: true},
		{format: "vtt", src: "/four.vtt", wantConvert: false},
	} {
		t.Run(tc.format, func(t *testing.T) {
			res := (&Api{}).makeSubtitleURL(base, ExtSubtitle{Src: tc.src, Format: tc.format})
			if got := strings.Contains(res, "~vtt/"); got != tc.wantConvert {
				t.Fatalf("~vtt conversion = %v, want %v; url: %s", got, tc.wantConvert, res)
			}
		})
	}
}

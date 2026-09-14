// This file is package template_test (not template) on purpose: it binds
// the real handlers/action.Helper methods (in particular GetSubtitles) into
// the FuncMap, and handlers/action imports services/template — so a file
// that needs both must live in the external test package to avoid an
// import cycle.
package template_test

import (
	"bytes"
	"encoding/json"
	"html/template"
	"strings"
	"testing"

	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/handlers/action"
	"github.com/webtor-io/web-ui/jobs/scripts"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
)

// TestStreamVideoRenders is the render guard called for in the
// subtitle-translate-webui rulings (R-D): getSubtitles is bound into the
// template FuncMap by reflection (services/template.Manager.WithHelper), so
// a wrong arity on action.Helper.GetSubtitles — e.g. this task's new
// trailing SubtitleOpts parameter landing on the Go side without the
// matching argument in the template call — type-checks fine and only blows
// up when the view is actually parsed/rendered. Modelled on
// user_subtitles_partial_render_test.go: parse the real template with a
// FuncMap of stubs for everything unrelated to this task, plus the real
// action.Helper methods so a broken getSubtitles call goes red here instead
// of only at server startup (tm.Init()) or in production.
//
// Coverage note: this exercises the "main" define of stream_video.html end
// to end (attributes, both dialogs' subtitle/audio loops, the two
// getSubtitles call sites) with a minimal StreamContent — MediaProbe nil,
// no OpenSubtitles/ExportTag/UserSubtitles/ext tracks, UserSubtitlesEnabled
// false so the my-subtitles partial (and its userSubtitleView/withContext
// call) is not entered. It does NOT cover: the my-subtitles branch's own
// markup (that partial has its own render test), the actual HTML the
// Forced/Locked/Badge fields produce (Task 5 adds that markup — nothing in
// this template reads them yet), or i18n string content (t/tp/tpHTML are
// echo stubs here).
func TestStreamVideoRenders(t *testing.T) {
	helper := action.NewHelper()

	echo := func(lang, key string) string { return key }
	echoVariadic := func(lang, key string, args ...interface{}) string { return key }
	echoHTML := func(lang, key string, args ...interface{}) template.HTML { return template.HTML(key) }

	funcs := template.FuncMap{
		// Real handlers/action.Helper methods -- this is the point of the
		// test: they must accept exactly the arguments the template passes.
		"getSubtitles":              helper.GetSubtitles,
		"getAudioTracks":            helper.GetAudioTracks,
		"hasControls":               helper.HasControls,
		"getDurationSec":            helper.GetDurationSec,
		"filterSubtitlesByProvider": helper.FilterSubtitlesByProvider,
		"userSubtitleView":          helper.UserSubtitleView,

		// Stubs for the web.Helper-bound funcs this view also needs, same
		// spirit as about_render_test.go / user_subtitles_partial_render_test.go:
		// only funcs the template manager actually registers belong here.
		"domain":      func() string { return "https://example.com" },
		"langPath":    func(lang, p string) string { return p },
		"json":        func(v interface{}) template.JS { return template.JS("{}") },
		"asset":       func(p string) template.HTML { return template.HTML(p) },
		"hasAuth":     func(interface{}) bool { return false },
		"withContext": func(ctx, data interface{}) interface{} { return map[string]interface{}{"Ctx": ctx, "Data": data} },
		"t":           echo,
		"tp":          echoVariadic,
		"tpHTML":      echoHTML,
	}

	tpl, err := template.New("stream_video.html").Funcs(funcs).
		ParseFiles("../../templates/views/action/stream_video.html")
	if err != nil {
		t.Fatalf("failed to parse stream_video.html: %v", err)
	}
	// user_subtitles_view is only reached when UserSubtitlesEnabled is true
	// (not the case below), but html/template's escaper walks every branch
	// of the parse tree up front, so the named template must exist for
	// *parsing* to succeed regardless of which branch runs.
	if _, err := tpl.Parse(`{{ define "user_subtitles_view" }}<!--stub-->{{ end }}`); err != nil {
		t.Fatalf("failed to define user_subtitles_view stub: %v", err)
	}

	data := &scripts.StreamContent{
		ExportTag:            &ra.ExportTag{},
		Resource:             &ra.ResourceResponse{},
		Item:                 &ra.ListItem{PathStr: "movie.mkv"},
		Title:                "Movie",
		MediaProbe:           nil,
		OpenSubtitles:        nil,
		UserSubtitles:        nil,
		UserSubtitlesEnabled: false,
		EIURL:                "http://ei.example.com",
		VideoStreamUserData:  &models.VideoStreamUserData{ResourceID: "res", ItemID: "item"},
		Settings:             &models.StreamSettings{},
		ExternalData:         &models.ExternalData{},
		DomainSettings:       nil,
		TranscoderSession:    nil,
		SubtitleOpts:         models.SubtitleOpts{},
	}

	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "main", map[string]interface{}{
		"Data": data,
		"Lang": "en",
		"User": nil,
	}); err != nil {
		t.Fatalf("failed to render stream_video.html: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("rendered stream_video.html is empty")
	}
}

// TestStreamVideoRendersTranslateBadgesAndCTA is Task 5's render guard: a
// fixture that makes handlers/action.Helper.GetSubtitles actually produce a
// Forced item, a Locked Translated item (with a non-empty SourceBadge), and
// checks that the template renders the data-rank/data-source-badge
// attributes, the lock, and the always-present (hidden) translate-cta card
// for those shapes -- none of which the base TestStreamVideoRenders fixture
// above exercises (it has no subtitles at all).
//
// Fixture: one audio track (eng) + two embedded subtitle streams (a plain
// English one and a "Forced (English)" one) + one ExportTag sidecar track in
// English, with SubtitleOpts{PreferredLang: "pt", Translate: true, Paid:
// false}. No human track exists in Portuguese, so the ladder adds a locked
// AI item translated from the English sidecar (mirrors
// handlers/action.TestLadderLockedForFree, which asserts the same opts
// produce Locked=true/Src=""/Default=true on the Go side).
func TestStreamVideoRendersTranslateBadgesAndCTA(t *testing.T) {
	helper := action.NewHelper()

	echo := func(lang, key string) string { return key }
	echoVariadic := func(lang, key string, args ...interface{}) string { return key }
	echoHTML := func(lang, key string, args ...interface{}) template.HTML { return template.HTML(key) }

	funcs := template.FuncMap{
		"getSubtitles":              helper.GetSubtitles,
		"getAudioTracks":            helper.GetAudioTracks,
		"hasControls":               helper.HasControls,
		"getDurationSec":            helper.GetDurationSec,
		"filterSubtitlesByProvider": helper.FilterSubtitlesByProvider,
		"userSubtitleView":          helper.UserSubtitleView,

		"domain":      func() string { return "https://example.com" },
		"langPath":    func(lang, p string) string { return p },
		"json":        func(v interface{}) template.JS { return template.JS("{}") },
		"asset":       func(p string) template.HTML { return template.HTML(p) },
		"hasAuth":     func(interface{}) bool { return false },
		"withContext": func(ctx, data interface{}) interface{} { return map[string]interface{}{"Ctx": ctx, "Data": data} },
		"t":           echo,
		"tp":          echoVariadic,
		"tpHTML":      echoHTML,
	}

	tpl, err := template.New("stream_video.html").Funcs(funcs).
		ParseFiles("../../templates/views/action/stream_video.html")
	if err != nil {
		t.Fatalf("failed to parse stream_video.html: %v", err)
	}
	if _, err := tpl.Parse(`{{ define "user_subtitles_view" }}<!--stub-->{{ end }}`); err != nil {
		t.Fatalf("failed to define user_subtitles_view stub: %v", err)
	}

	var mp api.MediaProbe
	if err := json.Unmarshal([]byte(`{"streams":[
		{"codec_type":"audio","codec_name":"aac","tags":{"language":"eng"}},
		{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","title":"English"}},
		{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","title":"Forced (English)"}}
	]}`), &mp); err != nil {
		t.Fatalf("failed to build MediaProbe fixture: %v", err)
	}

	data := &scripts.StreamContent{
		ExportTag: &ra.ExportTag{Tracks: []ra.ExportTrack{
			{Src: "https://x/sc-en.vtt", SrcLang: "en", Label: "Movie.srt", Kind: "subtitles"},
		}},
		Resource:             &ra.ResourceResponse{},
		Item:                 &ra.ListItem{PathStr: "movie.mkv"},
		Title:                "Movie",
		MediaProbe:           &mp,
		OpenSubtitles:        nil,
		UserSubtitles:        nil,
		UserSubtitlesEnabled: false,
		EIURL:                "http://ei.example.com",
		VideoStreamUserData:  &models.VideoStreamUserData{ResourceID: "res", ItemID: "item"},
		Settings:             &models.StreamSettings{},
		ExternalData:         &models.ExternalData{},
		DomainSettings:       nil,
		TranscoderSession:    nil,
		SubtitleOpts:         models.SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: false},
	}

	// Sanity-check the fixture actually produces the three shapes this test
	// means to exercise, so a change to the ladder that silently stops
	// producing them fails here with a clear message instead of a passing
	// render test that no longer covers anything.
	items := helper.GetSubtitles(data.VideoStreamUserData, data.MediaProbe, data.ExportTag, data.OpenSubtitles, data.ExternalData, data.UserSubtitles, data.SubtitleOpts)
	var forcedOK, translatedOK bool
	for _, it := range items {
		if it.Forced && it.Badge == "forced" {
			forcedOK = true
		}
		if it.Provider == "Translated" {
			if !it.Locked || it.Src != "" || it.SourceBadge == "" || it.Rank != 5 {
				t.Fatalf("fixture's Translated item does not have the expected shape: %+v", it)
			}
			translatedOK = true
		}
	}
	if !forcedOK {
		t.Fatal("fixture did not produce a Forced item -- test no longer covers the forced/locked/rank markup")
	}
	if !translatedOK {
		t.Fatal("fixture did not produce a Translated item -- test no longer covers the AI/lock/CTA markup")
	}

	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "main", map[string]interface{}{
		"Data": data,
		"Lang": "en",
		"User": nil,
	}); err != nil {
		t.Fatalf("failed to render stream_video.html: %v", err)
	}
	html := buf.String()

	for _, want := range []string{
		`data-rank="5"`,               // Translated item's ladder rank
		`data-source-badge="sidecar"`, // AI item translated from the ExportTag sidecar track
		`data-badge="forced"`,         // the embedded "Forced (English)" track
		`data-forced="true"`,
		`data-locked="true"`,
		`🔒`,                  // lock glyph on the locked AI item
		`id="translate-cta"`, // the CTA card
		`action.stream.translate.locked`,
		`action.stream.translate.cta`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered stream_video.html missing %q", want)
		}
	}
}

// TestStreamVideoRendersMySubtitlesTab executes the one call site the two
// tests above never reach: `userSubtitleView` inside the
// `{{ if and (not .DomainSettings) .UserSubtitlesEnabled }}` branch. Both of
// them set UserSubtitlesEnabled false, so the branch is parsed but never
// run — and that helper is bound by reflection like getSubtitles, so its
// arity (the ladder-result argument added for the Default/Saved copy) is
// only checked when the branch actually executes. A missing argument there
// type-checks fine and blows up at tm.Init() or in production.
//
// The real partial is stubbed the same way the other two tests stub it: the
// markup it produces has its own render test
// (user_subtitles_partial_render_test.go). What is under test here is the
// call, and that the ladder marks the upload it should — asserted directly
// on the helper's output, since the stub swallows the rendered row.
func TestStreamVideoRendersMySubtitlesTab(t *testing.T) {
	helper := action.NewHelper()

	echo := func(lang, key string) string { return key }
	echoVariadic := func(lang, key string, args ...interface{}) string { return key }
	echoHTML := func(lang, key string, args ...interface{}) template.HTML { return template.HTML(key) }

	funcs := template.FuncMap{
		"getSubtitles":              helper.GetSubtitles,
		"getAudioTracks":            helper.GetAudioTracks,
		"hasControls":               helper.HasControls,
		"getDurationSec":            helper.GetDurationSec,
		"filterSubtitlesByProvider": helper.FilterSubtitlesByProvider,
		"userSubtitleView":          helper.UserSubtitleView,

		"domain":      func() string { return "https://example.com" },
		"langPath":    func(lang, p string) string { return p },
		"json":        func(v interface{}) template.JS { return template.JS("{}") },
		"asset":       func(p string) template.HTML { return template.HTML(p) },
		"hasAuth":     func(interface{}) bool { return true },
		"withContext": func(ctx, data interface{}) interface{} { return map[string]interface{}{"Ctx": ctx, "Data": data} },
		"t":           echo,
		"tp":          echoVariadic,
		"tpHTML":      echoHTML,
	}

	tpl, err := template.New("stream_video.html").Funcs(funcs).
		ParseFiles("../../templates/views/action/stream_video.html")
	if err != nil {
		t.Fatalf("failed to parse stream_video.html: %v", err)
	}
	// The stub records that the branch was entered, so a template change
	// that quietly stops rendering the tab cannot leave this test passing
	// while covering nothing. An element, not an HTML comment: html/template
	// strips comments from its output, so a comment marker would never show
	// up however well the branch ran.
	if _, err := tpl.Parse(`{{ define "user_subtitles_view" }}<span data-stub="my-subtitles"></span>{{ end }}`); err != nil {
		t.Fatalf("failed to define user_subtitles_view stub: %v", err)
	}

	userSubs := []models.UserSubtitleTrack{
		{ID: "us-1", Label: "movie.pt.srt", OriginalName: "movie.pt.srt", SrcLang: "pt", Src: "https://x.test/ext/abc/movie.srt~vtt/movie.vtt", Format: "srt", Size: 1024, DeleteURL: "/user-subtitle/delete/1"},
	}
	data := &scripts.StreamContent{
		ExportTag:            &ra.ExportTag{},
		Resource:             &ra.ResourceResponse{},
		Item:                 &ra.ListItem{PathStr: "movie.mkv"},
		Title:                "Movie",
		MediaProbe:           nil,
		OpenSubtitles:        nil,
		UserSubtitles:        userSubs,
		UserSubtitlesEnabled: true,
		EIURL:                "http://ei.example.com",
		VideoStreamUserData:  &models.VideoStreamUserData{ResourceID: "res", ItemID: "item", SubtitleID: "us-1"},
		Settings:             &models.StreamSettings{},
		ExternalData:         &models.ExternalData{},
		DomainSettings:       nil,
		TranscoderSession:    nil,
		SubtitleOpts:         models.SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true},
	}

	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "main", map[string]interface{}{
		"Data": data,
		"Lang": "en",
		"User": struct{}{},
	}); err != nil {
		t.Fatalf("failed to render stream_video.html: %v", err)
	}
	if !strings.Contains(buf.String(), `data-stub="my-subtitles"`) {
		t.Fatalf("the my-subtitles branch was not entered, so userSubtitleView never ran:\n%s", buf.String())
	}

	// The seam the new argument exists for: the viewer's saved choice is
	// one of their own uploads, and the view model carries that across.
	items := helper.GetSubtitles(data.VideoStreamUserData, data.MediaProbe, data.ExportTag, data.OpenSubtitles, data.ExternalData, data.UserSubtitles, data.SubtitleOpts)
	v := helper.UserSubtitleView(data.VideoStreamUserData.ResourceID, data.Item.PathStr, data.EIURL, data.UserSubtitles, items)
	if len(v.UserSubtitles) != 1 || !v.UserSubtitles[0].Default || !v.UserSubtitles[0].Saved {
		t.Fatalf("the saved upload must reach the partial marked: %+v", v.UserSubtitles)
	}
}

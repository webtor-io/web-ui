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
	"github.com/webtor-io/web-ui/services/stremio"
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
		"subtitleLangGroups":        helper.SubtitleLangGroups,
		"originCode":                helper.OriginCode,
		"originCodeForBadge":        helper.OriginCodeForBadge,
		"originKey":                 helper.OriginKey,
		"propertyTags":              helper.PropertyTags,
		"audioSuffix":               helper.AudioSuffix,
		"langDisplay":               stremio.NewHelper().LangDisplay,

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
		"subtitleLangGroups":        helper.SubtitleLangGroups,
		"originCode":                helper.OriginCode,
		"originCodeForBadge":        helper.OriginCodeForBadge,
		"originKey":                 helper.OriginKey,
		"propertyTags":              helper.PropertyTags,
		"audioSuffix":               helper.AudioSuffix,
		"langDisplay":               stremio.NewHelper().LangDisplay,

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
		`aria-disabled="true"`, // a locked chip is not activatable
		`id="translate-cta"`,   // the CTA card
		`action.stream.translate.locked`,
		`action.stream.translate.cta`,
		`action.stream.locked.aria`, // the lock is an icon plus this sr-only text
		// The redesign's own contract: one flat container for the tracks, a
		// language row above it, and the codes the chips are read by.
		`id="subtitle-tracks"`,
		`id="subtitle-langs"`,
		`id="subtitle-off"`,
		`id="lang-chip-template"`,
		`class="lang lang-chip`,
		`class="subtitle track-chip`,
		`aria-pressed="true"`, // the expanded language chip
		// title= is what tells a chip's origin badge from the legend line,
		// which renders the same codes with no title at all.
		`title="action.stream.origin.em">EM<`,
		`title="action.stream.origin.in">IN<`,
		`title="action.stream.origin.ai">AI<`,
		`class="tr-progress`,
		`class="tr-spinner`,
		`data-lang="en"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered stream_video.html missing %q", want)
		}
	}

	// The sub-views the redesign replaced, and the roles ruling R2 replaced:
	// the language row is a filter (role="group" + aria-pressed), not a
	// tablist, and the Off chip is a chip of the track row rather than an
	// aria-owns reference from the language row.
	for _, gone := range []string{
		`id="embedded"`, `id="opensubtitles"`, `label for="opensubtitles"`, `label for="my-subtitles"`,
		`aria-owns=`, `role="tab"`, `aria-selected=`, `🔒`,
	} {
		if strings.Contains(html, gone) {
			t.Errorf("rendered stream_video.html still contains the removed markup %q", gone)
		}
	}

	tracksAt := strings.Index(html, `id="subtitle-tracks"`)
	if tracksAt < 0 {
		t.Fatal("no #subtitle-tracks container")
	}
	ctaAt := strings.Index(html, `id="translate-cta"`)
	if ctaAt < tracksAt {
		t.Fatal("the CTA card no longer follows the track row")
	}
	tracks := html[tracksAt:ctaAt]
	// hls-manager.remapTrackGroup assigns data-mp-id by DOM order when the
	// counts match, so the embedded chips must appear in GetSubtitles order.
	// Scoped to the track row on purpose: the audio chips above carry
	// mp-ids too, and an unscoped search would pass on those alone.
	if a, b := strings.Index(tracks, `data-mp-id="0"`), strings.Index(tracks, `data-mp-id="1"`); a < 0 || b < 0 || a > b {
		t.Errorf("embedded subtitle chips are out of GetSubtitles order (mp-0 at %d, mp-1 at %d)", a, b)
	}

	// startTag returns the <button …> opening tag of the chip carrying the
	// given attribute, without its closing ">". Searching the whole element
	// for "hidden" would prove nothing: chip-check, tr-progress, the "+N"
	// button and the CTA card all carry it.
	// Scoped to the track row: the audio chips above reuse the same
	// data-id values ("mp-0"), so an unscoped search would read the wrong
	// element and pass for the wrong reason.
	startTag := func(needle string) string {
		at := strings.Index(tracks, needle)
		if at < 0 {
			t.Fatalf("no chip with %s in the track row:\n%s", needle, tracks)
		}
		open := strings.LastIndex(tracks[:at], "<button")
		end := strings.Index(tracks[at:], ">")
		if open < 0 || end < 0 {
			t.Fatalf("could not isolate the start tag of %s", needle)
		}
		return tracks[open : at+end]
	}

	// The language filter runs server-side (R4): the expanded language is
	// Portuguese (the AI track is the default), so the three English chips
	// are collapsed and the Portuguese one is not.
	if tag := startTag(`data-id="mp-0"`); !strings.Contains(tag, " hidden") {
		t.Errorf("an English chip is not collapsed while Portuguese is expanded:\n%s", tag)
	}
	if tag := startTag(`data-id="et-1"`); !strings.Contains(tag, " hidden") {
		t.Errorf("the sidecar chip is not collapsed while Portuguese is expanded:\n%s", tag)
	}
	if tag := startTag(`data-id="tr-pt"`); strings.Contains(tag, " hidden") {
		t.Errorf("the expanded language's chip is collapsed:\n%s", tag)
	}
	// ...but "Off" is not a language and must never be collapsed by it:
	// otherwise a viewer whose expanded language is anything but "und"
	// cannot turn subtitles off at all without JavaScript. It lives in the
	// language row (first button there, next to the languages it switches
	// off), so it is looked up in that slice, not in the track row.
	langsAt := strings.Index(html, `id="subtitle-langs"`)
	if langsAt < 0 || langsAt > tracksAt {
		t.Fatalf("no language row before the track row (langs at %d, tracks at %d)", langsAt, tracksAt)
	}
	langs := html[langsAt:tracksAt]
	offAt := strings.Index(langs, `id="subtitle-off"`)
	if offAt < 0 {
		t.Fatalf("the Off chip is not in the language row:\n%s", langs)
	}
	if open := strings.LastIndex(langs[:offAt], "<button"); open < 0 || strings.Contains(langs[open:offAt+strings.Index(langs[offAt:], ">")], " hidden") {
		t.Errorf("the Off chip is hidden by the language filter:\n%s", langs)
	}
	if first := strings.Index(langs, "<button"); first < 0 || first != strings.LastIndex(langs[:offAt], "<button") {
		t.Errorf("the Off chip is not the first button of #subtitle-langs (first button at %d, off at %d)", first, offAt)
	}
	if strings.Contains(tracks, `id="subtitle-off"`) {
		t.Errorf("the Off chip is rendered twice (also inside #subtitle-tracks)")
	}

	// The "+N" disclosure's whole label lives in .more-count, because
	// track-picker.js rewrites that span's text on every refresh. A literal
	// "+" left outside it would survive the rewrite and read "++3".
	moreAt := strings.Index(html, `id="subtitle-lang-more"`)
	if moreAt < 0 {
		t.Fatal("no #subtitle-lang-more button")
	}
	moreEnd := strings.Index(html[moreAt:], "</button>") + moreAt
	if inner := html[moreAt+strings.Index(html[moreAt:], ">")+1 : moreEnd]; inner != `<span class="more-count">+0</span>` {
		// Task 3 rewrites .more-count wholesale, so the "+" belongs inside it.
		t.Errorf("the +N button must render its whole label inside .more-count, got %q", inner)
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
		"subtitleLangGroups":        helper.SubtitleLangGroups,
		"originCode":                helper.OriginCode,
		"originCodeForBadge":        helper.OriginCodeForBadge,
		"originKey":                 helper.OriginKey,
		"propertyTags":              helper.PropertyTags,
		"audioSuffix":               helper.AudioSuffix,
		"langDisplay":               stremio.NewHelper().LangDisplay,

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
	v := helper.UserSubtitleView(data.VideoStreamUserData.ResourceID, data.Item.PathStr, data.EIURL, data.UserSubtitles, items, "pt")
	if len(v.UserSubtitles) != 1 || !v.UserSubtitles[0].Default || !v.UserSubtitles[0].Saved {
		t.Fatalf("the saved upload must reach the partial marked: %+v", v.UserSubtitles)
	}
}

// TestStreamVideoRendersUploadChipsInsideTheTrackRow is the one case that
// renders the real user_subtitles_view partial inside the dialog instead of
// stubbing it. The redesign moved the uploads out of their own sub-view and
// into the flat track row, which puts two things at risk that a stub hides:
// the chips have to land inside #subtitle-tracks (the picker reads them
// there), and each upload must be rendered exactly once — the dialog
// filters UserSubtitle items out of its own loop precisely because the
// partial renders them.
func TestStreamVideoRendersUploadChipsInsideTheTrackRow(t *testing.T) {
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
		"subtitleLangGroups":        helper.SubtitleLangGroups,
		"originCode":                helper.OriginCode,
		"originCodeForBadge":        helper.OriginCodeForBadge,
		"originKey":                 helper.OriginKey,
		"propertyTags":              helper.PropertyTags,
		"audioSuffix":               helper.AudioSuffix,
		"langDisplay":               stremio.NewHelper().LangDisplay,

		"domain":        func() string { return "https://example.com" },
		"langPath":      func(lang, p string) string { return p },
		"json":          func(v interface{}) template.JS { return template.JS("{}") },
		"asset":         func(p string) template.HTML { return template.HTML(p) },
		"hasAuth":       func(interface{}) bool { return true },
		"bitsForHumans": func(int64) string { return "1 KB" },
		"withContext":   func(ctx, data interface{}) interface{} { return map[string]interface{}{"Ctx": ctx, "Data": data} },
		"t":             echo,
		"tp":            echoVariadic,
		"tpHTML":        echoHTML,
	}

	tpl, err := template.New("stream_video.html").Funcs(funcs).ParseFiles(
		"../../templates/views/action/stream_video.html",
		"../../templates/partials/action/user_subtitles.html",
	)
	if err != nil {
		t.Fatalf("failed to parse templates: %v", err)
	}

	userSubs := []models.UserSubtitleTrack{
		{ID: "us-1", Label: "movie.pt.srt", OriginalName: "movie.pt.srt", SrcLang: "pt", Src: "https://x.test/movie.vtt", Format: "srt", Size: 1024, DeleteURL: "/user-subtitle/delete/1"},
	}
	data := &scripts.StreamContent{
		ExportTag:            &ra.ExportTag{},
		Resource:             &ra.ResourceResponse{},
		Item:                 &ra.ListItem{PathStr: "movie.mkv"},
		Title:                "Movie",
		MediaProbe:           nil,
		UserSubtitles:        userSubs,
		UserSubtitlesEnabled: true,
		EIURL:                "http://ei.example.com",
		VideoStreamUserData:  &models.VideoStreamUserData{ResourceID: "res", ItemID: "item", SubtitleID: "us-1"},
		Settings:             &models.StreamSettings{},
		ExternalData:         &models.ExternalData{},
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
	html := buf.String()

	// Rendered once, by the partial — not once here and once by the
	// dialog's own loop.
	if n := strings.Count(html, `data-id="us-1"`); n != 1 {
		t.Errorf("the upload must be rendered exactly once, got %d:\n%s", n, html)
	}
	if !strings.Contains(html, `data-provider="UserSubtitle"`) {
		t.Fatal("the partial did not render the upload's chip")
	}

	tracksAt := strings.Index(html, `id="subtitle-tracks"`)
	chipAt := strings.Index(html, `data-provider="UserSubtitle"`)
	ctaAt := strings.Index(html, `id="translate-cta"`)
	if tracksAt < 0 || ctaAt < 0 {
		t.Fatal("the dialog is missing its track row or its CTA card")
	}
	if chipAt < tracksAt || chipAt > ctaAt {
		t.Errorf("the MY chip is outside #subtitle-tracks (row at %d, chip at %d, next block at %d)", tracksAt, chipAt, ctaAt)
	}
	// The uploads disclosure comes from the same partial, so an upload or a
	// delete re-sends it together with the chips.
	for _, want := range []string{`id="my-uploads-toggle"`, `id="my-uploads-panel"`, `action="/user-subtitle/delete/1"`} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered dialog missing %q", want)
		}
	}
}

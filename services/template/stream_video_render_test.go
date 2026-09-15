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

	// Every player dialog closes on a click outside its box (owner,
	// 2026-09-15). DaisyUI does that with one .modal-backdrop form as the
	// dialog's last child: it stretches across the same grid cell as the
	// .modal-box behind it (z-index -1), and its submit button closes the
	// dialog through method="dialog". Two of them in one dialog would put a
	// second full-size button over the box; none leaves the dialog
	// closable only by its own button.
	html := buf.String()
	parts := strings.Split(html, "<dialog")
	if len(parts) != 3 {
		t.Fatalf("expected 2 dialogs in stream_video.html, got %d", len(parts)-1)
	}
	for _, d := range parts[1:] {
		end := strings.Index(d, "</dialog>")
		if end < 0 {
			t.Fatalf("unterminated dialog: %.80s", d)
		}
		body := d[:end]
		id := body[:strings.Index(body, ">")]
		if n := strings.Count(body, `class="modal-backdrop"`); n != 1 {
			t.Errorf("dialog %s has %d .modal-backdrop forms, want exactly 1", id, n)
		}
		// Last child: the backdrop is stacked behind the box, and markup
		// order is what puts the box's own controls on top of it.
		if at := strings.LastIndex(body, `class="modal-backdrop"`); at >= 0 && strings.Contains(body[at:], "modal-box") {
			t.Errorf("dialog %s renders the backdrop before its box", id)
		}
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
		`id="subtitle-none"`,
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
		// The switch that replaced the Off chip. In this fixture the AI item
		// is locked and nothing matches Accept-Language, so the ladder lands
		// on "None": the switch renders off, both rows are muted, and the
		// track the switch would turn on carries data-suggested.
		`id="subtitles-toggle"`,
		`class="toggle toggle-soft toggle-sm">`,
		`data-subtitles-off="true"`,
		`class="lang-row flex flex-wrap items-center gap-1.5 picker-off"`,
		`id="subtitle-tracks" class="flex flex-wrap gap-1.5 mb-3 picker-off"`,
		`data-suggested="true"`,
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
		// The Off chip and its label: the switch on the heading replaced
		// both, and action.stream.off was dropped from every locale.
		`action.stream.off`,
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
	// ...but the "None" item is not a language either, and since the toggle
	// replaced its chip it is a hidden state carrier at the head of the
	// track row: the player still activates it by id, the viewer never
	// sees it, and the language row holds languages only.
	langsAt := strings.Index(html, `id="subtitle-langs"`)
	if langsAt < 0 || langsAt > tracksAt {
		t.Fatalf("no language row before the track row (langs at %d, tracks at %d)", langsAt, tracksAt)
	}
	langs := html[langsAt:tracksAt]
	if strings.Contains(langs, `data-id="none"`) {
		t.Errorf("the None item is back in the language row:\n%s", langs)
	}
	// The switch leads the language row (owner, 2026-09-16) — where the Off
	// chip used to be — and the heading line above carries nothing but the
	// title.
	toggleAt := strings.Index(langs, `id="subtitles-toggle"`)
	if toggleAt < 0 {
		t.Fatalf("the switch is not in the language row:\n%s", langs)
	}
	if firstLang := strings.Index(langs, `class="lang lang-chip`); firstLang >= 0 && toggleAt > firstLang {
		t.Errorf("the switch must come before the first language chip (toggle at %d, first chip at %d)", toggleAt, firstLang)
	}
	// ...and it is outside the part that dims: a control at half opacity is
	// the one thing that must stay legible while subtitles are off.
	rowAt := strings.Index(langs, `class="lang-row`)
	if rowAt < 0 || toggleAt > rowAt {
		t.Errorf("the switch must sit beside .lang-row, not inside it (toggle at %d, row at %d)", toggleAt, rowAt)
	}
	heading := html[strings.LastIndex(html[:langsAt], `<div class="flex items-baseline`):langsAt]
	if strings.Contains(heading, "subtitles-toggle") {
		t.Errorf("the heading line still carries the switch:\n%s", heading)
	}
	offAt := strings.Index(tracks, `id="subtitle-none"`)
	if offAt < 0 {
		t.Fatalf("the None carrier is not in the track row:\n%s", tracks)
	}
	if first := strings.Index(tracks, "<button"); first < 0 || first != strings.LastIndex(tracks[:offAt], "<button") {
		t.Errorf("the None carrier is not the first element of #subtitle-tracks (first button at %d, carrier at %d)", first, offAt)
	}
	if tag := startTag(`id="subtitle-none"`); !strings.Contains(tag, " hidden") || !strings.Contains(tag, `aria-hidden="true"`) {
		t.Errorf("the None carrier is not hidden from view and from readers:\n%s", tag)
	}

	// The suggested chip wears the active look while the block is muted, so
	// it has to carry the ARIA state that look means. A chip drawn as chosen
	// and announced as unchosen is the worst of both.
	if tag := startTag(`data-suggested="true"`); !strings.Contains(tag, `aria-checked="true"`) || !strings.Contains(tag, "track-chip-active") {
		t.Errorf("the suggested chip is not marked as the chosen one while muted:\n%s", tag)
	}
	if n := strings.Count(tracks, `aria-checked="true"`); n != 1 {
		t.Errorf("exactly one chip may be aria-checked in the track row, got %d", n)
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

// TestStreamVideoSubtitlesToggleFollowsTheDefault is the other half of the
// switch's contract: the fixture above renders it off (the ladder lands on
// "None"), this one renders it on. Same tracks, one difference — the viewer
// pays, so the AI translation is activatable and becomes the default.
//
// What it pins: the checkbox is checked exactly when something other than
// "None" is the default, the dialog says so in data-subtitles-off, neither
// row is muted, and nothing carries data-suggested — with subtitles on,
// data-default already answers "what is playing" and a second answer would
// let the picker restore something else.
func TestStreamVideoSubtitlesToggleFollowsTheDefault(t *testing.T) {
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
		{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","title":"English"}}
	]}`), &mp); err != nil {
		t.Fatalf("failed to build MediaProbe fixture: %v", err)
	}

	data := &scripts.StreamContent{
		ExportTag: &ra.ExportTag{Tracks: []ra.ExportTrack{
			{Src: "https://x/sc-en.vtt", SrcLang: "en", Label: "Movie.srt", Kind: "subtitles"},
		}},
		Resource:            &ra.ResourceResponse{},
		Item:                &ra.ListItem{PathStr: "movie.mkv"},
		Title:               "Movie",
		MediaProbe:          &mp,
		EIURL:               "http://ei.example.com",
		VideoStreamUserData: &models.VideoStreamUserData{ResourceID: "res", ItemID: "item"},
		Settings:            &models.StreamSettings{},
		ExternalData:        &models.ExternalData{},
		SubtitleOpts:        models.SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true},
	}

	// Sanity-check the fixture: the point of this test is the "on" state,
	// and a ladder change that made "None" the default here would leave it
	// silently asserting the same thing as the test above.
	items := helper.GetSubtitles(data.VideoStreamUserData, data.MediaProbe, data.ExportTag, data.OpenSubtitles, data.ExternalData, data.UserSubtitles, data.SubtitleOpts)
	def := ""
	for _, it := range items {
		if it.Default {
			def = it.ID
		}
	}
	if def == "" || def == "none" {
		t.Fatalf("fixture no longer has a track selected (default=%q) -- the test would cover the off state twice", def)
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
		`class="toggle toggle-soft toggle-sm" checked`,
		`data-subtitles-off="false"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered stream_video.html missing %q", want)
		}
	}
	for _, gone := range []string{
		` picker-off"`,
		`data-suggested="true"`,
	} {
		if strings.Contains(html, gone) {
			t.Errorf("subtitles are on, but the render still contains %q", gone)
		}
	}
}

// TestStreamVideoRendersASuggestedUpload is the one path the tests above
// stub out: the dialog with UserSubtitlesEnabled, rendering the REAL
// uploads partial, with the ladder's suggestion landing on an upload.
//
// That combination is the common one, not a corner: an upload is rank 0, so
// whenever the viewer has subtitles off and has ever uploaded a file in
// their language, the track the switch would turn on is that file — and its
// chip comes from a different template, fed by a different view model. The
// marker has to survive that hop, which is what this asserts end to end
// (helper → UserSubtitleView → partial → markup).
func TestStreamVideoRendersASuggestedUpload(t *testing.T) {
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
		"bitsForHumans": func(int64) string { return "1 KB" },
		// The uploads partial renders its chips only for a signed-in viewer.
		"hasAuth":     func(interface{}) bool { return true },
		"withContext": func(ctx, data interface{}) interface{} { return map[string]interface{}{"Ctx": ctx, "Data": data} },
		"t":           echo,
		"tp":          echoVariadic,
		"tpHTML":      echoHTML,
	}

	// The real partial this time, not the stub: its markup is what is under
	// test.
	tpl, err := template.New("stream_video.html").Funcs(funcs).
		ParseFiles("../../templates/views/action/stream_video.html", "../../templates/partials/action/user_subtitles.html")
	if err != nil {
		t.Fatalf("failed to parse templates: %v", err)
	}

	// Japanese audio, English preference, one English upload and one English
	// sidecar: the ladder prefers the upload (rank 0 beats rank 2), and the
	// viewer has subtitles off, so that upload is the suggestion.
	var mp api.MediaProbe
	if err := json.Unmarshal([]byte(`{"streams":[{"codec_type":"audio","codec_name":"aac","tags":{"language":"jpn"}}]}`), &mp); err != nil {
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
		UserSubtitles:        []models.UserSubtitleTrack{{ID: "us-1", OriginalName: "mine.en.srt", Format: "srt", Size: 10, Src: "https://x/mine.vtt", DeleteURL: "/d/1", SrcLang: "en"}},
		UserSubtitlesEnabled: true,
		EIURL:                "http://ei.example.com",
		VideoStreamUserData:  &models.VideoStreamUserData{ResourceID: "res", ItemID: "item", SubtitleID: "none"},
		Settings:             &models.StreamSettings{},
		ExternalData:         &models.ExternalData{},
		SubtitleOpts:         models.SubtitleOpts{PreferredLang: "en"},
	}

	// Fixture guard: the suggestion must actually be the upload, or this
	// test would pass while covering the sidecar chip the dialog renders
	// itself.
	items := helper.GetSubtitles(data.VideoStreamUserData, data.MediaProbe, data.ExportTag, data.OpenSubtitles, data.ExternalData, data.UserSubtitles, data.SubtitleOpts)
	for _, it := range items {
		if it.Suggested && it.ID != "us-1" {
			t.Fatalf("the fixture suggests %s, not the upload -- the partial hop is no longer covered", it.ID)
		}
	}

	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "main", map[string]interface{}{
		"Data": data,
		"Lang": "en",
		"User": struct{}{},
		"CSRF": "csrf",
	}); err != nil {
		t.Fatalf("failed to render stream_video.html: %v", err)
	}
	html := buf.String()

	at := strings.Index(html, `data-id="us-1"`)
	if at < 0 {
		t.Fatalf("the upload's chip was not rendered:\n%s", html)
	}
	chip := html[strings.LastIndex(html[:at], "<button"):]
	chip = chip[:strings.Index(chip, "</button>")]
	for _, want := range []string{`data-suggested="true"`, `aria-checked="true"`, "track-chip-active", `aria-disabled="true"`} {
		if !strings.Contains(chip, want) {
			t.Errorf("the suggested upload is missing %q:\n%s", want, chip)
		}
	}
	// One suggestion in the whole dialog: the sidecar must not carry it too.
	if n := strings.Count(html, `data-suggested="true"`); n != 1 {
		t.Errorf("expected exactly one suggested chip in the dialog, got %d", n)
	}
	if !strings.Contains(html, `data-subtitles-off="true"`) {
		t.Error("the switch must render off for a saved 'none'")
	}
}

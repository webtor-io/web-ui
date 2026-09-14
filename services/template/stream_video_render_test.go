// This file is package template_test (not template) on purpose: it binds
// the real handlers/action.Helper methods (in particular GetSubtitles) into
// the FuncMap, and handlers/action imports services/template — so a file
// that needs both must live in the external test package to avoid an
// import cycle.
package template_test

import (
	"bytes"
	"html/template"
	"testing"

	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/handlers/action"
	"github.com/webtor-io/web-ui/jobs/scripts"
	"github.com/webtor-io/web-ui/models"
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

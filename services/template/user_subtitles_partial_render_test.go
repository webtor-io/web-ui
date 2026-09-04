package template

import (
	"bytes"
	"html/template"
	"os"
	"strings"
	"testing"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/i18n"
)

// TestUserSubtitlesPartialMarksSelected pins the model→template seam that
// carries the "switch to this one" signal after an upload. The partial is
// rendered standalone for the same reason as the other partial tests here:
// it only appears inside the stream modal behind an auth + feature gate, so
// nothing exercises it at startup.
//
// Without data-autoselect the player has no way to tell which track the
// viewer just uploaded, and the upload silently changes nothing on screen —
// support ticket fd718d99.
func TestUserSubtitlesPartialMarksSelected(t *testing.T) {
	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatalf("locales: %v", err)
	}
	defer locales.Close()
	helper := i18n.NewHelper(i18n.New(locales.FS()))

	funcs := template.FuncMap{
		"t":             helper.T,
		"langPath":      func(lang, p string) string { return p },
		"hasAuth":       func(any) bool { return true },
		"bitsForHumans": func(int64) string { return "1 KB" },
	}
	tpl, err := template.New("user_subtitles.html").Funcs(funcs).
		ParseFiles("../../templates/partials/action/user_subtitles.html")
	if err != nil {
		t.Fatalf("failed to parse partial: %v", err)
	}

	data := &models.UserSubtitleView{
		ResourceID: "res",
		Path:       "/movie.mkv",
		EIURL:      "http://ei",
		UserSubtitles: []models.UserSubtitleTrack{
			{ID: "us-old", OriginalName: "old.srt", Format: "srt", Size: 10, Src: "http://a", DeleteURL: "/d/1", SrcLang: "und"},
			{ID: "us-new", OriginalName: "new.en.srt", Format: "srt", Size: 20, Src: "http://b", DeleteURL: "/d/2", Selected: true, SrcLang: "en"},
		},
	}

	var buf bytes.Buffer
	err = tpl.ExecuteTemplate(&buf, "user_subtitles_view", map[string]any{
		"Ctx":  map[string]any{"Lang": "en", "User": struct{}{}, "CSRF": "csrf"},
		"Data": data,
	})
	if err != nil {
		t.Fatalf("failed to render: %v", err)
	}
	out := buf.String()

	if strings.Count(out, `data-autoselect="true"`) != 1 {
		t.Errorf("expected exactly one autoselect marker, got %d:\n%s",
			strings.Count(out, `data-autoselect="true"`), out)
	}
	// The player reads the language off the list item when it creates the
	// <track> for a subtitle uploaded after the initial render.
	if !strings.Contains(out, `data-srclang="en"`) || !strings.Contains(out, `data-srclang="und"`) {
		t.Errorf("list items must expose srclang:\n%s", out)
	}

	// The marker has to sit on the uploaded track, not just anywhere.
	newIdx := strings.Index(out, `data-id="us-new"`)
	oldIdx := strings.Index(out, `data-id="us-old"`)
	markIdx := strings.Index(out, `data-autoselect="true"`)
	if newIdx < 0 || oldIdx < 0 || markIdx < newIdx {
		t.Errorf("autoselect marker is not on the uploaded track:\n%s", out)
	}
}

// Negative control: an ordinary list render (initial page, or reload after a
// delete) marks nothing, so re-rendering never steals the viewer's choice.
func TestUserSubtitlesPartialMarksNothingByDefault(t *testing.T) {
	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatalf("locales: %v", err)
	}
	defer locales.Close()
	helper := i18n.NewHelper(i18n.New(locales.FS()))

	tpl, err := template.New("user_subtitles.html").Funcs(template.FuncMap{
		"t":             helper.T,
		"langPath":      func(lang, p string) string { return p },
		"hasAuth":       func(any) bool { return true },
		"bitsForHumans": func(int64) string { return "1 KB" },
	}).ParseFiles("../../templates/partials/action/user_subtitles.html")
	if err != nil {
		t.Fatalf("failed to parse partial: %v", err)
	}

	var buf bytes.Buffer
	err = tpl.ExecuteTemplate(&buf, "user_subtitles_view", map[string]any{
		"Ctx": map[string]any{"Lang": "en", "User": struct{}{}, "CSRF": "csrf"},
		"Data": &models.UserSubtitleView{
			ResourceID: "res", Path: "/movie.mkv", EIURL: "http://ei",
			UserSubtitles: []models.UserSubtitleTrack{
				{ID: "us-1", OriginalName: "a.srt", Format: "srt", Size: 10, Src: "http://a", DeleteURL: "/d/1"},
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to render: %v", err)
	}
	if strings.Contains(buf.String(), "data-autoselect") {
		t.Errorf("plain render must not mark any track:\n%s", buf.String())
	}
}

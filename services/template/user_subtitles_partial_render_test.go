package template

import (
	"bytes"
	"html/template"
	"os"
	"strings"
	"testing"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/stremio"
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
		"langDisplay":   stremio.NewHelper().LangDisplay,
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
	// Every upload row carries the "mine" origin badge the picker shows
	// next to the label (one per rendered track).
	if n := strings.Count(out, `data-badge="user"`); n != 2 {
		t.Errorf("expected two user badges, got %d:\n%s", n, out)
	}
	// The player reads the language off the list item when it creates the
	// <track> for a subtitle uploaded after the initial render.
	if !strings.Contains(out, `data-srclang="en"`) || !strings.Contains(out, `data-srclang="und"`) {
		t.Errorf("list items must expose srclang:\n%s", out)
	}

	// The uploads disclosure is part of this partial, not of the dialog:
	// it is re-sent on every async reload, so a viewer who just uploaded a
	// file still has the panel (and its delete controls) to work with.
	if !strings.Contains(out, `id="my-uploads-toggle"`) {
		t.Error("partial must render the My-uploads chip")
	}
	if !strings.Contains(out, `id="my-uploads-panel"`) {
		t.Error("partial must render the My-uploads panel")
	}
	// One selection chip per upload, drawn like every other chip in the
	// track row it is rendered into.
	if n := strings.Count(out, `class="subtitle track-chip`); n != 2 {
		t.Errorf("expected two MY chips, got %d:\n%s", n, out)
	}
	// The chips carry the language they group under, so the picker's
	// language row can count them without re-deriving the tag.
	if !strings.Contains(out, `data-lang="en"`) {
		t.Error("MY chips must carry the language they group under")
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
		"langDisplay":   stremio.NewHelper().LangDisplay,
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

// TestUserSubtitlesPartialMarksDefaultAndSaved covers the markers the
// player's audio-switch rule reads off this list. The "My Subtitles" tab
// renders from its own view model, so before UserSubtitleTrack carried
// Default/Saved a viewer whose saved choice was one of their own uploads
// had a list where nothing was marked: pickDefaultSubtitle saw no default
// to keep and hasSavedDefault saw no saved choice to respect, so switching
// the audio track re-decided over an explicit choice.
func TestUserSubtitlesPartialMarksDefaultAndSaved(t *testing.T) {
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
		"langDisplay":   stremio.NewHelper().LangDisplay,
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
				{ID: "us-other", OriginalName: "other.srt", Format: "srt", Size: 10, Src: "http://a", DeleteURL: "/d/1"},
				{ID: "us-chosen", OriginalName: "chosen.srt", Format: "srt", Size: 20, Src: "http://b", DeleteURL: "/d/2", Default: true, Saved: true},
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to render: %v", err)
	}
	out := buf.String()

	// Exactly one of each, and both on the chosen row.
	if n := strings.Count(out, `data-default="true"`); n != 1 {
		t.Errorf("expected exactly one default marker, got %d:\n%s", n, out)
	}
	if n := strings.Count(out, `data-saved="true"`); n != 1 {
		t.Errorf("expected exactly one saved marker, got %d:\n%s", n, out)
	}
	chosen := strings.Index(out, `data-id="us-chosen"`)
	other := strings.Index(out, `data-id="us-other"`)
	if chosen < 0 || other < 0 || other > chosen {
		t.Fatalf("fixture order changed:\n%s", out)
	}
	if d := strings.Index(out, `data-default="true"`); d < chosen {
		t.Errorf("default marker is not on the chosen upload:\n%s", out)
	}
	if sv := strings.Index(out, `data-saved="true"`); sv < chosen {
		t.Errorf("saved marker is not on the chosen upload:\n%s", out)
	}
}

// TestUserSubtitlesPartialSavedIsIndependentOfDefault pins that the two
// markers are separate signals: a row can be playing without being the
// viewer's own choice (a ladder pick), and the audio rule is allowed to
// override exactly that case.
func TestUserSubtitlesPartialSavedIsIndependentOfDefault(t *testing.T) {
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
		"langDisplay":   stremio.NewHelper().LangDisplay,
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
				{ID: "us-1", OriginalName: "a.srt", Format: "srt", Size: 10, Src: "http://a", DeleteURL: "/d/1", Default: true},
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to render: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `data-default="true"`) {
		t.Errorf("a ladder pick still renders as the default:\n%s", out)
	}
	if strings.Contains(out, `data-saved`) {
		t.Errorf("a ladder pick is not the viewer's saved choice:\n%s", out)
	}
}

// TestUserSubtitlesPartialKeepsADeleteControlPerRow pins the half of the
// redesign that is easy to lose: the picker's chips are radios, so the
// delete form had to move into the "My uploads" panel — and a panel that
// silently stopped rendering it would leave viewers unable to remove a
// file they uploaded, with nothing failing anywhere.
//
// It also pins the separation: no chip may carry a delete form. An
// accidental tap on a selection chip must at worst switch the track.
func TestUserSubtitlesPartialKeepsADeleteControlPerRow(t *testing.T) {
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
		"langDisplay":   stremio.NewHelper().LangDisplay,
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
			{ID: "us-new", OriginalName: "new.en.srt", Format: "srt", Size: 20, Src: "http://b", DeleteURL: "/d/2", SrcLang: "en"},
		},
	}

	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "user_subtitles_view", map[string]any{
		"Ctx":  map[string]any{"Lang": "en", "User": struct{}{}, "CSRF": "csrf"},
		"Data": data,
	}); err != nil {
		t.Fatalf("failed to render: %v", err)
	}
	out := buf.String()

	if n := strings.Count(out, `action="/d/`); n != 2 {
		t.Errorf("expected one delete form per upload row, got %d:\n%s", n, out)
	}
	if n := strings.Count(out, `data-umami-event="user-subtitle-delete"`); n != 2 {
		t.Errorf("expected one delete button per upload row, got %d", n)
	}
	if n := strings.Count(out, `data-async-target="#my-subtitles"`); n != 3 {
		t.Errorf("expected two delete forms + the upload form to reload the picker, got %d", n)
	}
	// The delete forms must live in the panel, after it opens — never
	// inside a selection chip.
	panelAt := strings.Index(out, `id="my-uploads-panel"`)
	if panelAt < 0 {
		t.Fatal("no My-uploads panel")
	}
	if first := strings.Index(out, `action="/d/`); first < panelAt {
		t.Errorf("a delete form is rendered before the panel (at %d, panel at %d) — it must not sit on a chip", first, panelAt)
	}
}

// TestUserSubtitlesPartialCollapsesChipsOutsideTheExpandedLanguage pins the
// half of the language filter that runs outside stream_video.html: the MY
// chips are rendered into the same track row and have to be collapsed by
// the same rule, or a page served without JavaScript shows one language's
// tracks plus every upload the viewer ever made.
//
// ExpandedLang is empty on the async reload (the /user-subtitle handler has
// no language row to consult), and then nothing is collapsed — the client
// re-applies the filter right after the swap.
func TestUserSubtitlesPartialCollapsesChipsOutsideTheExpandedLanguage(t *testing.T) {
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
		"langDisplay":   stremio.NewHelper().LangDisplay,
	}).ParseFiles("../../templates/partials/action/user_subtitles.html")
	if err != nil {
		t.Fatalf("failed to parse partial: %v", err)
	}

	render := func(expanded string) string {
		var buf bytes.Buffer
		if err := tpl.ExecuteTemplate(&buf, "user_subtitles_view", map[string]any{
			"Ctx": map[string]any{"Lang": "en", "User": struct{}{}, "CSRF": "csrf"},
			"Data": &models.UserSubtitleView{
				ResourceID: "res", Path: "/movie.mkv", EIURL: "http://ei",
				ExpandedLang: expanded,
				UserSubtitles: []models.UserSubtitleTrack{
					{ID: "us-en", OriginalName: "a.en.srt", Format: "srt", Size: 10, Src: "http://a", DeleteURL: "/d/1", SrcLang: "en"},
					{ID: "us-pt", OriginalName: "b.pt.srt", Format: "srt", Size: 20, Src: "http://b", DeleteURL: "/d/2", SrcLang: "pt"},
				},
			},
		}); err != nil {
			t.Fatalf("failed to render: %v", err)
		}
		return buf.String()
	}

	startTag := func(out, id string) string {
		at := strings.Index(out, id)
		if at < 0 {
			t.Fatalf("fixture did not render %s:\n%s", id, out)
		}
		return out[at : at+strings.Index(out[at:], ">")]
	}

	out := render("en")
	if tag := startTag(out, `data-id="us-en"`); strings.Contains(tag, "hidden") {
		t.Errorf("the expanded language's upload is collapsed:\n%s", tag)
	}
	if tag := startTag(out, `data-id="us-pt"`); !strings.Contains(tag, "hidden") {
		t.Errorf("an upload outside the expanded language is not collapsed:\n%s", tag)
	}

	// The async reload carries no expanded language: collapse nothing
	// rather than everything.
	out = render("")
	for _, id := range []string{`data-id="us-en"`, `data-id="us-pt"`} {
		if tag := startTag(out, id); strings.Contains(tag, "hidden") {
			t.Errorf("an async reload must not collapse anything:\n%s", tag)
		}
	}
}

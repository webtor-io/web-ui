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
		// The chips are the async render's job now: on a page load the
		// dialog's own track loop emits them inside the radiogroup and this
		// partial contributes only the disclosure and the panel. Everything
		// below is about the markup of a reload.
		RenderChips: true,
		ResourceID:  "res",
		Path:        "/movie.mkv",
		EIURL:       "http://ei",
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
			// The chips are the async render's job now: on a page load the
			// dialog's own track loop emits them inside the radiogroup and this
			// partial contributes only the disclosure and the panel. Everything
			// below is about the markup of a reload.
			RenderChips: true,
			ResourceID:  "res", Path: "/movie.mkv", EIURL: "http://ei",
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
			// The chips are the async render's job now: on a page load the
			// dialog's own track loop emits them inside the radiogroup and this
			// partial contributes only the disclosure and the panel. Everything
			// below is about the markup of a reload.
			RenderChips: true,
			ResourceID:  "res", Path: "/movie.mkv", EIURL: "http://ei",
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
			// The chips are the async render's job now: on a page load the
			// dialog's own track loop emits them inside the radiogroup and this
			// partial contributes only the disclosure and the panel. Everything
			// below is about the markup of a reload.
			RenderChips: true,
			ResourceID:  "res", Path: "/movie.mkv", EIURL: "http://ei",
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
		// The chips are the async render's job now: on a page load the
		// dialog's own track loop emits them inside the radiogroup and this
		// partial contributes only the disclosure and the panel. Everything
		// below is about the markup of a reload.
		RenderChips: true,
		ResourceID:  "res",
		Path:        "/movie.mkv",
		EIURL:       "http://ei",
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
				// The chips are the async render's job now: on a page load the
				// dialog's own track loop emits them inside the radiogroup and this
				// partial contributes only the disclosure and the panel. Everything
				// below is about the markup of a reload.
				RenderChips: true,
				ResourceID:  "res", Path: "/movie.mkv", EIURL: "http://ei",
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

// TestUserSubtitlesPartialHasOneCloseControlPerPanel: the panel is a
// disclosure the viewer opened from a chip, and the way back was the same
// chip — findable only if you remember pressing it. It now carries an
// explicit "×" in its heading line (owner, 2026-09-15). Exactly one per
// rendered panel: two would be two ways to say the same thing in the same
// corner, and the delegate in Player.jsx binds by id.
func TestUserSubtitlesPartialHasOneCloseControlPerPanel(t *testing.T) {
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

	for _, tc := range []struct {
		name string
		subs []models.UserSubtitleTrack
	}{
		{"with uploads", []models.UserSubtitleTrack{{ID: "us-1", OriginalName: "a.srt", Format: "srt", Size: 10, Src: "http://a", DeleteURL: "/d/1", SrcLang: "en"}}},
		{"empty", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err = tpl.ExecuteTemplate(&buf, "user_subtitles_view", map[string]any{
				"Ctx":  map[string]any{"Lang": "en", "User": struct{}{}, "CSRF": "csrf"},
				"Data": &models.UserSubtitleView{ResourceID: "res", Path: "/movie.mkv", EIURL: "http://ei", UserSubtitles: tc.subs},
			})
			if err != nil {
				t.Fatalf("failed to render: %v", err)
			}
			out := buf.String()

			if n := strings.Count(out, `id="my-uploads-close"`); n != 1 {
				t.Fatalf("expected exactly one panel close control, got %d:\n%s", n, out)
			}
			panelAt := strings.Index(out, `id="my-uploads-panel"`)
			closeAt := strings.Index(out, `id="my-uploads-close"`)
			if panelAt < 0 || closeAt < panelAt {
				t.Errorf("the close control must be inside the panel (panel at %d, close at %d)", panelAt, closeAt)
			}
			// In the heading line, so it reads as the panel's own control
			// and not as something belonging to the first upload row.
			listAt := strings.Index(out, "<ul")
			if listAt >= 0 && closeAt > listAt {
				t.Errorf("the close control must sit in the heading line, above the uploads list")
			}
			// It closes the panel; it must never be mistaken for a submit
			// inside the upload or delete forms.
			openAt := strings.LastIndex(out[:closeAt], "<button")
			btn := out[openAt : closeAt+strings.Index(out[closeAt:], ">")]
			for _, want := range []string{`type="button"`, "btn-xs", `aria-label="Close"`} {
				if !strings.Contains(btn, want) {
					t.Errorf("close control missing %q:\n%s", want, btn)
				}
			}
		})
	}
}

// TestUserSubtitlesPartialMarksTheSuggestion: an upload is rank 0, so it is
// the likeliest thing the subtitles switch would turn on — and until the
// partial renders data-suggested, the one chip most often suggested was the
// one chip the client could not find. The muted look travels with it: while
// subtitles are off the suggested chip wears the check and the fill, here
// exactly as in the dialog's own track row.
func TestUserSubtitlesPartialMarksTheSuggestion(t *testing.T) {
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

	render := func(v *models.UserSubtitleView) string {
		var buf bytes.Buffer
		if err := tpl.ExecuteTemplate(&buf, "user_subtitles_view", map[string]any{
			"Ctx":  map[string]any{"Lang": "en", "User": struct{}{}, "CSRF": "csrf"},
			"Data": v,
		}); err != nil {
			t.Fatalf("failed to render: %v", err)
		}
		return buf.String()
	}

	subs := []models.UserSubtitleTrack{
		{ID: "us-1", OriginalName: "a.srt", Format: "srt", Size: 10, Src: "http://a", DeleteURL: "/d/1", SrcLang: "en", Suggested: true},
		{ID: "us-2", OriginalName: "b.srt", Format: "srt", Size: 10, Src: "http://b", DeleteURL: "/d/2", SrcLang: "en"},
	}
	out := render(&models.UserSubtitleView{ResourceID: "res", Path: "/m.mkv", EIURL: "http://ei", UserSubtitles: subs, SubtitlesOff: true, RenderChips: true})

	if n := strings.Count(out, `data-suggested="true"`); n != 1 {
		t.Fatalf("expected exactly one suggested chip, got %d:\n%s", n, out)
	}
	at := strings.Index(out, `data-id="us-1"`)
	chip := out[strings.LastIndex(out[:at], "<button"):]
	chip = chip[:strings.Index(chip, "</button>")]
	for _, want := range []string{`data-suggested="true"`, "track-chip-active", `aria-checked="true"`} {
		if !strings.Contains(chip, want) {
			t.Errorf("the suggested upload is missing %q while subtitles are off:\n%s", want, chip)
		}
	}
	// The other upload stays unmarked: one answer per row.
	at2 := strings.Index(out, `data-id="us-2"`)
	chip2 := out[strings.LastIndex(out[:at2], "<button"):]
	chip2 = chip2[:strings.Index(chip2, "</button>")]
	if strings.Contains(chip2, "track-chip-active") || strings.Contains(chip2, `aria-checked="true"`) {
		t.Errorf("a chip that is neither playing nor suggested is marked:\n%s", chip2)
	}

	// Negative control: subtitles on. Nothing is suggested then — Default
	// alone says what is playing — so the flag must not paint a second
	// chosen-looking chip.
	subs[0].Suggested = false
	subs[1].Default = true
	on := render(&models.UserSubtitleView{ResourceID: "res", Path: "/m.mkv", EIURL: "http://ei", UserSubtitles: subs, RenderChips: true})
	if strings.Contains(on, `data-suggested="true"`) {
		t.Errorf("nothing may be suggested while subtitles are on:\n%s", on)
	}
	if n := strings.Count(on, "track-chip-active"); n != 1 {
		t.Errorf("expected exactly one active chip with subtitles on, got %d", n)
	}
}

// elementHTML balances <div>…</div> from the opening tag at `open` and
// returns that element's markup. It answers what a substring search cannot:
// whether something is INSIDE a container or merely after it. A twin lives
// in stream_video_render_test.go, which is package template_test — the two
// files cannot share a helper.
func elementHTML(html, open string) string {
	start := strings.Index(html, open)
	if start < 0 {
		return ""
	}
	depth := 0
	for i := start; i < len(html); i++ {
		if strings.HasPrefix(html[i:], "<div") {
			depth++
		} else if strings.HasPrefix(html[i:], "</div>") {
			depth--
			if depth == 0 {
				return html[start : i+len("</div>")]
			}
		}
	}
	return ""
}

// TestUserSubtitlesPartialRenderChipsSplitsTheTwoRenders pins the field the
// a11y fix turns on (ruling R8). The partial now renders into a wrapper that
// sits AFTER #subtitle-tracks, because a role="radiogroup" holds radios and
// nothing else and this partial also emits a disclosure button and a panel
// with two kinds of form. The chips it emits ARE radios, so who renders them
// depends on which render this is:
//
//   - initial page render (RenderChips false): the dialog's own track loop
//     already put every upload inside the row. A second copy here would be
//     the same file twice on screen, and in the wrong container — so this
//     render must emit the panel and nothing else;
//   - async reload (RenderChips true): nothing re-runs that loop, so the
//     chips come from here, wrapped in #my-upload-chips. The wrapper is not
//     decoration: its presence is what tells the client this response is the
//     complete current list, which is the only way a delete — a response
//     with one chip fewer, or none — can take the chip out of the row.
func TestUserSubtitlesPartialRenderChipsSplitsTheTwoRenders(t *testing.T) {
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

	subs := []models.UserSubtitleTrack{
		{ID: "us-1", OriginalName: "a.srt", Format: "srt", Size: 10, Src: "http://a", DeleteURL: "/d/1", SrcLang: "en"},
	}
	render := func(chips bool) string {
		var buf bytes.Buffer
		if err := tpl.ExecuteTemplate(&buf, "user_subtitles_view", map[string]any{
			"Ctx": map[string]any{"Lang": "en", "User": struct{}{}, "CSRF": "csrf"},
			"Data": &models.UserSubtitleView{
				ResourceID: "res", Path: "/m.mkv", EIURL: "http://ei",
				UserSubtitles: subs, RenderChips: chips,
			},
		}); err != nil {
			t.Fatalf("failed to render: %v", err)
		}
		return buf.String()
	}

	initial := render(false)
	if strings.Contains(initial, `id="my-upload-chips"`) {
		t.Errorf("the initial render must not carry the async marker:\n%s", initial)
	}
	if strings.Contains(initial, `class="subtitle`) || strings.Contains(initial, `data-provider="UserSubtitle"`) {
		t.Errorf("the initial render must not repeat the chips the dialog already rendered:\n%s", initial)
	}
	// What it does still owe: the disclosure, the panel, and the per-file
	// delete form — the whole reason this partial is in the page at all.
	for _, want := range []string{`id="my-uploads-toggle"`, `id="my-uploads-panel"`, `action="/d/1"`, `class="user-subtitle-form"`} {
		if !strings.Contains(initial, want) {
			t.Errorf("the initial render is missing %q:\n%s", want, initial)
		}
	}

	async := render(true)
	if n := strings.Count(async, `id="my-upload-chips"`); n != 1 {
		t.Fatalf("expected exactly one async chips marker, got %d:\n%s", n, async)
	}
	// The chips are INSIDE the marker: the client moves that element's
	// contents into the radiogroup and then drops it, so a chip rendered
	// beside it would be left behind in the uploads block.
	wrap := elementHTML(async, `<div id="my-upload-chips"`)
	if !strings.Contains(wrap, `data-id="us-1"`) || !strings.Contains(wrap, `data-provider="UserSubtitle"`) {
		t.Errorf("the chip is not inside #my-upload-chips:\n%s", async)
	}
	if strings.Contains(wrap, `id="my-uploads-toggle"`) || strings.Contains(wrap, "<form") {
		t.Errorf("only radios belong in the marker the client moves into the radiogroup:\n%s", wrap)
	}
	// An empty list still renders the marker: that is the delete case, and
	// an absent marker would read as "nothing was re-sent".
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "user_subtitles_view", map[string]any{
		"Ctx": map[string]any{"Lang": "en", "User": struct{}{}, "CSRF": "csrf"},
		"Data": &models.UserSubtitleView{
			ResourceID: "res", Path: "/m.mkv", EIURL: "http://ei", RenderChips: true,
		},
	}); err != nil {
		t.Fatalf("failed to render: %v", err)
	}
	if !strings.Contains(buf.String(), `id="my-upload-chips"`) {
		t.Errorf("a delete that emptied the list must still say so:\n%s", buf.String())
	}
}

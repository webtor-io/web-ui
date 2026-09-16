// This file is package template_test for the same reason
// stream_video_render_test.go is: it binds the real handlers/action.Helper
// into the FuncMap, and handlers/action imports services/template.
package template_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/handlers/action"
	"github.com/webtor-io/web-ui/jobs/scripts"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/stremio"
)

// The files the JS harness renders into jsdom. They are committed, because
// `npm test` must not need a Go toolchain.
const (
	dialogFixturePath      = "../../assets/src/js/lib/player/__fixtures__/subtitles-dialog.html"
	uploadsFixturePath     = "../../assets/src/js/lib/player/__fixtures__/user-subtitles-async.html"
	uploadsEmptyFixturePat = "../../assets/src/js/lib/player/__fixtures__/user-subtitles-async-empty.html"
)

// regenCmd is the exact, copy-pasteable command that rewrites the fixtures.
// The -ldflags matter: without them the test binary panics at init on the
// proto registration conflict between the abuse-store and torrent-store
// protobufs (see the Makefile's `test` target, which passes the same flag
// for the same reason). Printed verbatim in the failure below, because that
// message is what a developer reads at the moment they need it — a doc
// comment they have to go and find is not the same thing.
const regenCmd = `UPDATE_FIXTURES=1 go test ` +
	`-ldflags '-X google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=ignore' ` +
	`./services/template/ -run TestSubtitlesDialogFixture`

// TestSubtitlesDialogFixtureIsCurrent keeps the picker markup the JS wiring
// tests run against honest.
//
// Why a fixture at all. assets/src/js/lib/player/Player.wiring.test.js
// exercises wireTrackHandlers against a real DOM, and the whole point is
// that the DOM is the one Go emits: a hand-written fixture drifts silently,
// and a test that passes on markup the server stopped producing is worse
// than no test — it reports the wiring as covered.
//
// How it works. This test renders the dialog from a fixed StreamContent and
// compares it byte for byte with the committed file. A template change that
// touches the picker turns it red here, printing `regenCmd` (above) as the
// instruction to regenerate.
//
// Then re-run `npm test`: if the wiring tests go red on the new markup, the
// template change broke the player, which is exactly what this is for. The
// regeneration is deliberately a separate, explicit act — a test that
// rewrote its own expectation on every run would be green by construction.
//
// What the fixture holds, and why each part is there:
//
//   - two audio chips (Japanese, English) with their inner spans — the
//     click delegate has to survive a click landing on a <span>;
//   - eight subtitle languages, so #subtitle-lang-more renders a real "+N";
//   - a Japanese sidecar, embedded Japanese, OpenSubtitles in six
//     languages, one MY upload and one AI translation — every origin the
//     picker draws differently;
//   - the AI item unlocked and Offered, which is the state a click on it
//     has to turn into a poll;
//   - subtitles off (the ladder has nothing in Portuguese but the offer),
//     so the switch's on/off path has something to restore.
func TestSubtitlesDialogFixtureIsCurrent(t *testing.T) {
	// One upload in the dialog, and the async reload of the uploads partial
	// that the wiring tests replay: the upload case (the file just added is
	// Selected, so the response carries data-autoselect) and the delete case
	// that emptied the list. Hand-writing that response in the JS test was
	// exactly the drift this machinery exists to stop, applied to the one
	// shape the whole adoption mechanism turns on.
	fixtures := map[string]string{
		dialogFixturePath:      renderSubtitlesDialog(t),
		uploadsFixturePath:     renderUploadsAsync(t, asyncUploads()),
		uploadsEmptyFixturePat: renderUploadsAsync(t, nil),
	}

	for path, got := range fixtures {
		if os.Getenv("UPDATE_FIXTURES") != "" {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatalf("failed to create the fixture directory: %v", err)
			}
			if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
				t.Fatalf("failed to write %s: %v", path, err)
			}
			t.Logf("wrote %s (%d bytes)", path, len(got))
			continue
		}

		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read %s -- regenerate with:\n\t%s\n%v", path, regenCmd, err)
		}
		if string(want) != got {
			t.Fatalf("the picker markup changed and %s is stale.\n"+
				"Regenerate it with:\n\t%s\n"+
				"then re-run `npm test` -- if Player.wiring.test.js goes red on the new markup,\n"+
				"the template change broke the player wiring.", path, regenCmd)
		}
	}
}

// TestRegenCommandCarriesTheProtoLdflags keeps the printed instruction
// runnable.
//
// The first version of that message omitted -ldflags, so the one command a
// developer copies at the moment the fixture goes stale died on the proto
// registration conflict — the exact panic the Makefile target exists to
// avoid. Asserting against the Makefile rather than against a second copy
// of the string means a change to the flag reddens this instead of silently
// making the message wrong again.
func TestRegenCommandCarriesTheProtoLdflags(t *testing.T) {
	mk, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatalf("Makefile: %v", err)
	}
	var flags string
	for _, line := range strings.Split(string(mk), "\n") {
		if after, ok := strings.CutPrefix(line, "PROTO_CONFLICT_LDFLAGS :="); ok {
			flags = strings.TrimSpace(after)
			break
		}
	}
	if flags == "" {
		t.Fatal("PROTO_CONFLICT_LDFLAGS is gone from the Makefile -- if the proto conflict was fixed, drop the flag here too")
	}
	if !strings.Contains(regenCmd, "-ldflags '"+flags+"'") {
		t.Errorf("the regeneration command does not carry the Makefile's ldflags and will panic when run.\nMakefile: %s\ncommand:  %s", flags, regenCmd)
	}
	if !strings.Contains(regenCmd, "UPDATE_FIXTURES=1") || !strings.Contains(regenCmd, "-run TestSubtitlesDialogFixture") {
		t.Errorf("the regeneration command does not regenerate anything: %s", regenCmd)
	}
}

// TestSubtitlesDialogFixtureCoversWhatTheWiringTestsNeed is the guard on the
// guard. The fixture above is only useful while it still contains the states
// the JS tests drive; a StreamContent tweak that quietly dropped the AI item
// or the "+N" overflow would leave those tests passing against markup that
// no longer exercises anything, and the byte comparison alone cannot say so.
func TestSubtitlesDialogFixtureCoversWhatTheWiringTestsNeed(t *testing.T) {
	html := renderSubtitlesDialog(t)

	for _, want := range []string{
		`id="subtitles"`,
		`id="subtitles-toggle"`,
		`data-subtitles-off="true"`,
		`id="subtitle-none"`,
		`id="subtitle-lang-more"`,
		`id="my-subtitles"`,
		`id="my-uploads-toggle"`,
		`id="my-uploads-panel"`,
		`id="subtitle-hint"`,
		`id="translate-cta"`,
		`data-provider="UserSubtitle"`,
		`data-provider="Translated"`,
		`data-provider="MediaProbe"`,
		`data-provider="OpenSubtitles"`,
		`data-offered="true"`,
		"chip-offered",
		"tr-progress",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the fixture no longer carries %q, so the wiring test that drives it covers nothing", want)
		}
	}
	// More languages than the row shows, or the "+N" test toggles a button
	// that was never collapsing anything. Counted, not matched against a
	// whitespace-exact copy of the rendered button: a template reformat plus
	// a regeneration would quietly turn that into "the id exists".
	// 6 mirrors maxVisibleLangChips (handlers/action/picker.go), which is
	// unexported, and MAX_VISIBLE_LANGS (track-picker.js). Three copies of
	// one number already; a fourth is not worth exporting a constant for.
	const rowShows = 6
	langs := strings.Count(html, `class="lang lang-chip`)
	if langs <= rowShows {
		t.Errorf("the fixture renders %d language chips and the row shows %d: nothing overflows, so +N is inert",
			langs, rowShows)
	}
	if !strings.Contains(html, `<span class="more-count">+`) {
		t.Error(`the "+N" button must render a real count`)
	}
	// Two audio chips: one to start on, one to switch to.
	if n := strings.Count(html, `class="audio track-chip`); n < 2 {
		t.Errorf("expected at least two audio chips, got %d", n)
	}
}

// asyncUploads is the list an upload's response carries: the file that was
// already there, plus the one just added — Selected, which renders as
// data-autoselect and is what tells the client which chip to switch to.
// The ids match the dialog fixture's own upload, so replaying this response
// against that dialog exercises the replace-in-place path and not only the
// add path.
func asyncUploads() []models.UserSubtitleTrack {
	return []models.UserSubtitleTrack{
		{ID: "us-1", Label: "my.en.srt", OriginalName: "my.en.srt", SrcLang: "en",
			Src: "https://x.test/my.vtt", Format: "srt", Size: 2048, DeleteURL: "/user-subtitle/delete/1"},
		{ID: "us-2", Label: "fresh.en.srt", OriginalName: "fresh.en.srt", SrcLang: "en",
			Src: "https://x.test/fresh.vtt", Format: "srt", Size: 4096, DeleteURL: "/user-subtitle/delete/2",
			Selected: true},
	}
}

// renderUploadsAsync renders the uploads partial the way the async reload
// does: RenderChips true, no ExpandedLang and no SubtitlesOff — there is no
// ladder result to read on that path. It mirrors
// handlers/user_subtitle.buildView, which is unexported and in a package
// this one cannot import; TestBuildViewMarksTheListAuthoritative over there
// pins the producer's own half of the agreement.
func renderUploadsAsync(t *testing.T, subs []models.UserSubtitleTrack) string {
	t.Helper()
	helper := i18n.NewHelper(i18n.New(localesFS(t)))

	funcs := template.FuncMap{
		"t":             helper.T,
		"langPath":      func(lang, p string) string { return p },
		"hasAuth":       func(interface{}) bool { return true },
		"bitsForHumans": func(int64) string { return "1 KB" },
		"langDisplay":   stremio.NewHelper().LangDisplay,
	}
	tpl, err := template.New("user_subtitles.html").Funcs(funcs).
		ParseFiles("../../templates/partials/action/user_subtitles.html")
	if err != nil {
		t.Fatalf("failed to parse the uploads partial: %v", err)
	}

	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "user_subtitles_view", map[string]any{
		"Ctx": map[string]any{"Lang": "en", "User": struct{}{}, "CSRF": "csrf"},
		"Data": &models.UserSubtitleView{
			ResourceID:    "res",
			Path:          "movie.mkv",
			EIURL:         "http://ei.example.com",
			UserSubtitles: subs,
			RenderChips:   true,
		},
	}); err != nil {
		t.Fatalf("failed to render the uploads partial: %v", err)
	}
	return buf.String()
}

func localesFS(t *testing.T) fs.FS {
	t.Helper()
	root, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatalf("locales: %v", err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root.FS()
}

// renderSubtitlesDialog renders stream_video.html with the real partial and
// returns just the <dialog id="subtitles"> element — the JS harness injects
// exactly that into jsdom, and carrying the <video> and the embed dialog
// along would make the fixture churn on changes that have nothing to do with
// the picker.
func renderSubtitlesDialog(t *testing.T) string {
	t.Helper()
	helper := action.NewHelper()

	// The same echo stubs as the other render tests: i18n content is not
	// what the wiring reads, and real strings would make the fixture churn
	// on every copy change. tp echoes the substituted value so a sentence
	// built from a language name is still distinguishable.
	echo := func(lang, key string) string { return key }
	echoVariadic := func(lang, key string, args ...interface{}) string {
		out := key
		for i := 1; i < len(args); i += 2 {
			out += ":" + fmt.Sprint(args[i])
		}
		return out
	}
	echoHTML := func(lang, key string, args ...interface{}) template.HTML { return template.HTML(key) }

	funcs := template.FuncMap{
		"getSubtitles":       helper.GetSubtitles,
		"getAudioTracks":     helper.GetAudioTracks,
		"hasControls":        helper.HasControls,
		"getDurationSec":     helper.GetDurationSec,
		"userSubtitleView":   helper.UserSubtitleView,
		"subtitleLangGroups": helper.SubtitleLangGroups,
		"originCode":         helper.OriginCode,
		"originCodeForBadge": helper.OriginCodeForBadge,
		"originKey":          helper.OriginKey,
		"propertyTags":       helper.PropertyTags,
		"audioSuffix":        helper.AudioSuffix,
		"langDisplay":        stremio.NewHelper().LangDisplay,

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

	// Japanese audio and an English dub, an embedded Japanese subtitle
	// stream, a Japanese sidecar, OpenSubtitles in six more languages, the
	// viewer's own English upload, and a Portuguese viewer who pays — so the
	// only thing in their language is the AI item, which the server offers
	// and never starts.
	var mp api.MediaProbe
	if err := json.Unmarshal([]byte(`{"streams":[
		{"codec_type":"audio","codec_name":"aac","tags":{"language":"jpn","title":"Japanese"}},
		{"codec_type":"audio","codec_name":"aac","tags":{"language":"eng","title":"English dub"}},
		{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"jpn","title":"Japanese"}}
	]}`), &mp); err != nil {
		t.Fatalf("failed to build the MediaProbe fixture: %v", err)
	}

	os6 := []api.OpenSubtitleTrack{}
	for _, l := range []struct{ code, label string }{
		{"en", "Movie.en.srt"},
		{"ru", "Movie.ru.srt"},
		{"de", "Movie.de.srt"},
		{"fr", "Movie.fr.srt"},
		{"es", "Movie.es.srt"},
		{"it", "Movie.it.srt"},
	} {
		os6 = append(os6, api.OpenSubtitleTrack{
			ID:     "os-" + l.code,
			Source: "opensubtitles",
			ExportTrack: &ra.ExportTrack{
				Src: "https://x.test/os-" + l.code + ".vtt", SrcLang: l.code, Label: l.label, Kind: "subtitles",
			},
		})
	}

	data := &scripts.StreamContent{
		ExportTag: &ra.ExportTag{Tracks: []ra.ExportTrack{
			{Src: "https://x.test/sc-ja.vtt", SrcLang: "ja", Label: "Movie.ja.srt", Kind: "subtitles"},
		}},
		Resource:      &ra.ResourceResponse{},
		Item:          &ra.ListItem{PathStr: "movie.mkv"},
		Title:         "Movie",
		MediaProbe:    &mp,
		OpenSubtitles: os6,
		UserSubtitles: []models.UserSubtitleTrack{
			{ID: "us-1", Label: "my.en.srt", OriginalName: "my.en.srt", SrcLang: "en",
				Src: "https://x.test/my.vtt", Format: "srt", Size: 2048, DeleteURL: "/user-subtitle/delete/1"},
		},
		UserSubtitlesEnabled: true,
		EIURL:                "http://ei.example.com",
		VideoStreamUserData:  &models.VideoStreamUserData{ResourceID: "res", ItemID: "item"},
		Settings:             &models.StreamSettings{},
		ExternalData:         &models.ExternalData{},
		SubtitleOpts:         models.SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true},
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

	start := strings.Index(html, `<dialog class="modal" id="subtitles"`)
	if start < 0 {
		t.Fatalf("the subtitles dialog was not rendered:\n%s", html)
	}
	end := strings.Index(html[start:], "</dialog>")
	if end < 0 {
		t.Fatalf("the subtitles dialog is unclosed:\n%s", html[start:])
	}
	return html[start:start+end+len("</dialog>")] + "\n"
}

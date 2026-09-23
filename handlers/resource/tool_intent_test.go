package resource

import (
	"bytes"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/handlers/common"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/job"
	w "github.com/webtor-io/web-ui/services/web"
)

// The tool a visitor submitted a torrent on survives the submit (docs/
// tool_pages.md): the progress page keeps the tool's hero and <title>, the
// slug rides to the resource page as ?tool=, and there /magnet-to-torrent
// gets a line offering the files themselves.

const intentHash = "08ada5a7a6183aae1e09d831df6748d566095a10"

var allLangs = []string{"en", "ru", "es", "de", "fr", "pt", "it", "pl", "tr", "nl", "cs"}

// helperFuncs registers a helper's methods the way template.Manager.WithHelper
// does (serve.go): every exported method under its name with the first letter
// lowered. Real helpers rather than stubs, so a field path or an argument type
// that is wrong fails here and not on the live page.
func helperFuncs(funcs template.FuncMap, helpers ...any) {
	for _, h := range helpers {
		hv := reflect.ValueOf(h)
		ht := hv.Type()
		for i := 0; i < ht.NumMethod(); i++ {
			name := ht.Method(i).Name
			r, size := utf8.DecodeRuneInString(name)
			funcs[string(unicode.ToLower(r))+name[size:]] = hv.Method(i).Interface()
		}
	}
}

func intentFuncs(t *testing.T) template.FuncMap {
	t.Helper()
	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatalf("locales: %v", err)
	}
	t.Cleanup(func() { _ = locales.Close() })
	funcs := template.FuncMap{}
	helperFuncs(funcs, &w.Helper{}, i18n.NewHelper(i18n.New(locales.FS())), NewHelper("test-secret"))
	// Beside the point here, and the zero web.Helper has no asset map.
	funcs["asset"] = func(string) template.HTML { return "" }
	funcs["promoOffer"] = func() any { return nil }
	return funcs
}

// stubTemplates defines, as empty, the templates a view calls that are not
// what a test is about. html/template only notices a missing one when it is
// executed, so a stub keeps the test about its subject.
func stubTemplates(t *testing.T, tpl *template.Template, names ...string) {
	t.Helper()
	for _, n := range names {
		if _, err := tpl.Parse(`{{ define "` + n + `" }}{{ end }}`); err != nil {
			t.Fatalf("stub %s: %v", n, err)
		}
	}
}

// resourceTemplates parses the resource view with the partials its header and
// #content render.
func resourceTemplates(t *testing.T) *template.Template {
	t.Helper()
	tpl, err := template.New("get.html").Funcs(intentFuncs(t)).ParseFiles(
		"../../templates/views/resource/get.html",
		"../../templates/partials/resource/tool_intent.html",
		"../../templates/partials/file.html",
		"../../templates/partials/list.html",
		"../../templates/partials/button.html",
		"../../templates/partials/stream_button.html",
		"../../templates/partials/icons.html",
	)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	stubTemplates(t, tpl, "resource/status", "resource/piece_bar", "promo", "vault/button", "library/button",
		"user_video_status/movie_button", "user_video_status/series_button", "user_video_status/rate_button",
		"resource/release_subscribe_banner", "get_ads", "vault/pledge-modal", "user_video_status/rate-modal", "get_extra")
	return tpl
}

// intentPage is a resource page as prepareGetData leaves it. multi: a
// two-file torrent (the list, and the video picked as the item); otherwise a
// single-file one (the item only).
func intentPage(tool string, multi bool) *GetData {
	video := ra.ListItem{ID: "item-mp4", Name: "Sintel.mp4", PathStr: "/Sintel/Sintel.mp4", Type: ra.ListTypeFile, Size: 129241752, MediaFormat: ra.Video}
	gd := &GetData{
		Args: &GetArgs{ID: intentHash, Page: 1, PageSize: pageSize, FromTool: common.ToolByURL(tool)},
		Resource: &ExtendedResource{ResourceResponse: &ra.ResourceResponse{
			ID: intentHash, Name: "Sintel", MagnetURI: "magnet:?xt=urn:btih:" + intentHash, MultiFile: multi,
		}},
	}
	if multi {
		subs := ra.ListItem{ID: "item-srt", Name: "Sintel.en.srt", PathStr: "/Sintel/Sintel.en.srt", Type: ra.ListTypeFile, Size: 1652}
		gd.List = &ra.ListResponse{
			ListItem: ra.ListItem{ID: "dir-sintel", Name: "Sintel", PathStr: "/Sintel", Type: ra.ListTypeDirectory, Size: video.Size + subs.Size},
			Items:    []ra.ListItem{video, subs},
			Count:    2,
		}
		gd.Item = &gd.List.Items[0]
	} else {
		gd.Item = &video
	}
	return gd
}

func pageContext(lang string, gd *GetData) *w.Context {
	return &w.Context{Lang: lang, Data: gd, User: &auth.User{}}
}

func execute(t *testing.T, tpl *template.Template, name string, data any) string {
	t.Helper()
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, name, data); err != nil {
		t.Fatalf("render %s: %v", name, err)
	}
	return buf.String()
}

func TestNewPostDataResolvesTheTool(t *testing.T) {
	j := &job.Job{ID: "j", Queue: "load"}
	d := newPostData(j, &PostArgs{Instruction: "magnet-to-torrent"})
	if d.Tool == nil || d.Tool.Url != "magnet-to-torrent" || d.Instruction != "magnet-to-torrent" {
		t.Errorf("a tool page's submit must render that tool: Tool=%v Instruction=%q", d.Tool, d.Instruction)
	}
	if d.Job != j {
		t.Error("the job must reach the view")
	}
	// The home page sends an empty instruction; anything that is not a
	// registered tool is treated the same — home hero, home body.
	for _, v := range []string{"", "not-a-tool", "/magnet-to-torrent", "<script>"} {
		d := newPostData(j, &PostArgs{Instruction: v})
		if d.Tool != nil || d.Instruction != "" {
			t.Errorf("instruction %q: Tool=%v Instruction=%q, want the home page", v, d.Tool, d.Instruction)
		}
	}
}

// The submit answers with the index view and a job log. Before, the answer
// to a /magnet-to-torrent submit carried the home page's H1 and <title> while
// the address bar still said /magnet-to-torrent (a 202: async.js does not
// push it).
func TestProgressPageKeepsTheToolPage(t *testing.T) {
	funcs := intentFuncs(t)
	tr := funcs["t"].(func(string, string) string)
	tpl, err := template.New("index.html").Funcs(funcs).ParseFiles(
		"../../templates/views/index.html",
		"../../templates/partials/load/progress.html",
		"../../templates/partials/about/heading.html",
	)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	stubTemplates(t, tpl, "hero_wave", "onboarding_checklist", "continue_watching", "discover", "promo", "about")
	j := &job.Job{ID: "j1", Queue: "load"}
	render := func(instruction string) (title, main string) {
		ctx := &w.Context{Lang: "en", Data: newPostData(j, &PostArgs{Instruction: instruction})}
		return strings.TrimSpace(execute(t, tpl, "title", ctx)), execute(t, tpl, "main", ctx)
	}

	title, main := render("magnet-to-torrent")
	// Title + " – " + Benefit: the <title> the tool page itself has.
	if want := "Magnet to Torrent – Convert Magnet Link to .Torrent File"; title != want {
		t.Errorf("title = %q, want %q", title, want)
	}
	if !strings.Contains(main, `<span class="gradient-text">Convert Magnet Link to .Torrent File</span>`) {
		t.Errorf("the H1 is not the tool's benefit:\n%s", main)
	}
	if !strings.Contains(main, `data-async-progress-log="/queue/load/job/j1/log"`) {
		t.Error("the job log host is missing")
	}
	if !strings.Contains(main, `<input type="hidden" name="tool" value="magnet-to-torrent" />`) {
		t.Error("the log host does not carry the tool to the resource page")
	}
	// A second submit from this page is still a /magnet-to-torrent submit.
	if n := strings.Count(main, `<input name="instruction" value="magnet-to-torrent" type="hidden" />`); n != 3 {
		t.Errorf("%d of the 3 forms carry the instruction", n)
	}

	for _, instruction := range []string{"", "not-a-tool"} {
		title, main := render(instruction)
		if want := template.HTMLEscapeString(tr("en", "home.title")); title != want {
			t.Errorf("instruction %q: title = %q, want the home page's", instruction, title)
		}
		if strings.Contains(main, `name="tool"`) {
			t.Errorf("instruction %q: the home page's submit must not carry a tool", instruction)
		}
		if strings.Contains(main, "gradient-text\">Convert Magnet") {
			t.Errorf("instruction %q rendered a tool hero", instruction)
		}
	}
}

func TestBindGetArgsReadsTheTool(t *testing.T) {
	gin.SetMode(gin.TestMode)
	bind := func(query string) *GetArgs {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, "/"+intentHash+query, nil)
		c.Params = gin.Params{{Key: "resource_id", Value: intentHash}}
		args, err := (&Handler{}).bindGetArgs(c)
		if err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		// The layout's canonical/hreflang and the nav's language links are
		// built from the path alone, so the parameter cannot reach them.
		if p := w.NewContext(c).Path; p != "/"+intentHash {
			t.Errorf("%s: context path = %q, want the bare resource path", query, p)
		}
		return args
	}
	if a := bind("?tool=magnet-to-torrent"); a.FromTool == nil || a.FromTool.Url != "magnet-to-torrent" {
		t.Errorf("?tool=magnet-to-torrent: FromTool = %v", a.FromTool)
	}
	for _, q := range []string{"", "?tool=", "?tool=evil", "?tool=%2Fmagnet-to-torrent", "?from=magnet-to-torrent"} {
		if a := bind(q); a.FromTool != nil {
			t.Errorf("%q: FromTool = /%s, want nil", q, a.FromTool.Url)
		}
	}
}

func TestToolIntentLine(t *testing.T) {
	tpl := resourceTemplates(t)
	line := func(lang, tool string, gd *GetData) string {
		if gd == nil {
			gd = intentPage(tool, true)
		}
		return strings.TrimSpace(execute(t, tpl, "resource/tool_intent", pageContext(lang, gd)))
	}

	out := line("en", "magnet-to-torrent", nil)
	for _, want := range []string{
		`data-tool-intent="magnet-to-torrent"`,
		"Or download the files directly — no torrent client needed",
		`href="#content"`,
		`data-tool-intent-direct`,
		`data-umami-event="tool-intent-direct"`,
		`data-umami-event-tool="magnet-to-torrent"`,
		">\n        Download\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	// A single file has something to download too.
	if single := line("en", "magnet-to-torrent", intentPage("magnet-to-torrent", false)); !strings.Contains(single, "data-tool-intent-direct") {
		t.Errorf("single-file torrent: no line\n%s", single)
	}

	for _, tc := range []struct {
		name string
		gd   *GetData
	}{
		{"arrival not from a tool page", intentPage("", true)},
		{"another tool", intentPage("torrent-to-zip", true)},
		{"an unknown slug", intentPage("evil", true)},
		{"nothing to download", func() *GetData { d := intentPage("magnet-to-torrent", true); d.List, d.Item = nil, nil; return d }()},
		{"no args", func() *GetData { d := intentPage("magnet-to-torrent", true); d.Args = nil; return d }()},
	} {
		if got := line("en", "", tc.gd); got != "" {
			t.Errorf("%s: want no line, got\n%s", tc.name, got)
		}
	}

	for _, lang := range allLangs {
		out := line(lang, "magnet-to-torrent", nil)
		if strings.Contains(out, "resource.") || strings.Contains(out, "<no value>") {
			t.Errorf("%s: an untranslated key or a missing value reached the page:\n%s", lang, out)
		}
		if lang != "en" && strings.Contains(out, "no torrent client needed") {
			t.Errorf("%s: the line is not translated", lang)
		}
	}
}

// The .torrent button and the magnet button carry the tool as an analytics
// prop; from /torrent-to-magnet the magnet button also shows its label. For
// any other visit the pair renders as it did.
func TestTorrentMagnetSplitCarriesTheTool(t *testing.T) {
	tpl := resourceTemplates(t)
	split := func(tool string) string {
		gd := intentPage(tool, true)
		ctx := pageContext("en", gd)
		return execute(t, tpl, "resource/torrent_magnet_split", (&w.Helper{}).WithContext(ctx, gd.Resource))
	}

	plain := split("")
	for _, banned := range []string{"data-umami-event-tool", "data-tool=", "<span>Copy magnet link</span>", "border-green-400 text-green-400"} {
		if strings.Contains(plain, banned) {
			t.Errorf("a plain visit must not render %q:\n%s", banned, plain)
		}
	}
	if !strings.Contains(plain, "btn btn-sm btn-ghost border border-w-line text-w-sub rounded-none -ml-px") {
		t.Errorf("the magnet button lost its usual look:\n%s", plain)
	}

	m2t := split("magnet-to-torrent")
	if !strings.Contains(m2t, `data-umami-event="download-torrent" data-umami-event-tool="magnet-to-torrent"`) {
		t.Errorf(".torrent does not carry the tool:\n%s", m2t)
	}
	if !strings.Contains(m2t, `data-tool="magnet-to-torrent"`) || strings.Contains(m2t, "<span>Copy magnet link</span>") {
		t.Errorf("magnet button from /magnet-to-torrent: the prop without the label expected:\n%s", m2t)
	}

	t2m := split("torrent-to-magnet")
	for _, want := range []string{`data-tool="torrent-to-magnet"`, "<span>Copy magnet link</span>", "border-green-400 text-green-400 whitespace-nowrap"} {
		if !strings.Contains(t2m, want) {
			t.Errorf("magnet button from /torrent-to-magnet: missing %q:\n%s", want, t2m)
		}
	}
}

// The markup assets/src/js/lib/toolIntent.test.js runs against. Committed,
// because `npm test` must not need a Go toolchain; generated, because the
// script presses the page's own download buttons (form.download-dir in #list,
// form.download in #file) and a hand-written copy of those forms would keep
// passing after the real ones changed.
const toolIntentFixtureRegen = `UPDATE_FIXTURES=1 go test ` +
	`-ldflags '-X google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=ignore' ` +
	`./handlers/resource/ -run TestToolIntentFixturesAreCurrent`

func TestToolIntentFixturesAreCurrent(t *testing.T) {
	tpl := resourceTemplates(t)
	for name, multi := range map[string]bool{"multi": true, "single": false} {
		ctx := pageContext("en", intentPage("magnet-to-torrent", multi))
		var b strings.Builder
		b.WriteString("<!-- Generated by handlers/resource TestToolIntentFixturesAreCurrent — do not edit.\n     " + toolIntentFixtureRegen + " -->\n")
		b.WriteString(`<header id="resource-header">` + execute(t, tpl, "resource/tool_intent", ctx) + "</header>\n")
		// The wrapper is views/resource/get.html's #content; what it holds
		// is rendered.
		b.WriteString(`<div id="content" class="scroll-mt-36">` + execute(t, tpl, "resource/content", ctx) + "</div>\n")
		got := b.String()

		path := "../../assets/src/js/lib/__fixtures__/tool-intent-" + name + ".html"
		if os.Getenv("UPDATE_FIXTURES") != "" {
			if err := os.MkdirAll("../../assets/src/js/lib/__fixtures__", 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v — generate it with:\n\n    %s", name, err, toolIntentFixtureRegen)
		}
		if string(want) != got {
			t.Errorf("%s: %s is stale. Regenerate it, then run `npm test` — a red toolIntent test means the template change broke the button:\n\n    %s", name, path, toolIntentFixtureRegen)
		}
	}
}

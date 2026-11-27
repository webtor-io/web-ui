package template

import (
	"bytes"
	"fmt"
	"html/template"
	"maps"
	"os"
	"reflect"
	"strings"
	"testing"

	hc "github.com/webtor-io/web-ui/handlers/common"
)

// indexData mirrors handlers/index.Data (importing it would be a cycle): the
// fields views/index.html reads on the page a visitor lands on.
type indexData struct {
	Instruction string
	Tool        *hc.Tool
	// Job, Args and Onboarding are read through `has`; nil pointers keep
	// the job log and the checklist out of the render.
	Job              *struct{}
	Args             *struct{ Query string }
	Onboarding       *struct{}
	ContinueWatching []struct{}
}

// renderIndexMain executes views/index.html's "main" with the translation
// funcs echoing their key, and the sub-templates it pulls in stubbed out.
func renderIndexMain(t *testing.T, tool *hc.Tool, extra ...map[string]interface{}) string {
	t.Helper()
	return renderIndexMainWith(t, tool, nil, extra...)
}

// renderIndexMainWith is renderIndexMain with funcs put over its stubs.
func renderIndexMainWith(t *testing.T, tool *hc.Tool, over template.FuncMap, extra ...map[string]interface{}) string {
	t.Helper()
	echo := func(lang, key string, args ...interface{}) string { return key }
	funcs := template.FuncMap{
		"t": echo,
		"tn": func(lang, key string, count int, args ...interface{}) string {
			return fmt.Sprintf("tn(%s,%d,%v)", key, count, args)
		},
		"langPath": func(lang, p string) string { return p },
		// Same semantics as web.Helper.Has.
		"has": func(obj any, field string) bool {
			f := reflect.Indirect(reflect.ValueOf(obj)).FieldByName(field)
			return f.IsValid() && !f.IsNil()
		},
		"asset":        func(p string) template.HTML { return "" },
		"demoMagnet":   func() string { return "magnet:?xt=urn:btih:demo" },
		"seoFriendly":  func(s string) string { return s },
		"canonicalURL": func(lang, p string) string { return p },
		"domain":       func() string { return "" },
		"isPaid":       func(any) bool { return true },
		"promoOffer":   func() bool { return false },
		"withContext": func(ctx, data interface{}) interface{} {
			return map[string]interface{}{"Ctx": ctx, "Data": data}
		},
		// Uploads are off here: their markup is TestDropzoneUploads' subject.
		"uploadsEnabled": func() bool { return false },
		"uploadSignIn":   func() any { return nil },
		"hasAuth":        func(any) bool { return false },
		"tp":             func(lang, key string, args ...interface{}) string { return key },
		"uploadCap":      func(any) string { return "" },
	}
	maps.Copy(funcs, over)
	b, err := os.ReadFile("../../templates/views/index.html")
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := template.New("index").Funcs(funcs).Parse(string(b))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tpl.ParseFiles("../../templates/partials/about/heading.html"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"hero_backdrop", "hero_wave", "load/progress", "promo", "onboarding_checklist", "continue_watching", "discover", "about", "private/refusal", "upload/log"} {
		if _, err := tpl.Parse(`{{ define "` + name + `" }}{{ end }}`); err != nil {
			t.Fatal(err)
		}
	}
	ctx := map[string]interface{}{
		"Lang": "en",
		"Data": &indexData{Tool: tool},
	}
	for _, e := range extra {
		for k, v := range e {
			ctx[k] = v
		}
	}
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "main", ctx); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

// submitButton cuts the magnet form's submit button out of the page.
func submitButton(t *testing.T, page string) string {
	t.Helper()
	i := strings.Index(page, `<button type="submit" class="btn join-item btn-pink`)
	if i < 0 {
		t.Fatal("no submit button in the magnet form")
	}
	j := strings.Index(page[i:], "</button>")
	return page[i : i+j]
}

const loupePath = `M21 21l-4.35-4.35`

// Tool pages open a magnet; the home page keeps its search wording until the
// measurement window there closes (2026-10-02). Both keep the "search" event
// name so the Umami series does not break.
func TestIndexFormSaysOpenOnToolPagesOnly(t *testing.T) {
	home := renderIndexMain(t, nil)
	homeButton := submitButton(t, home)
	if !strings.Contains(homeButton, "home.search") || strings.Contains(homeButton, "home.open") {
		t.Errorf("home page button changed: %s", homeButton)
	}
	if !strings.Contains(home, loupePath) {
		t.Error("home page lost its loupe")
	}
	if strings.Contains(homeButton, "data-umami-event-page") {
		t.Error("home page button gained an event prop")
	}

	for _, tool := range hc.Tools {
		// No magnet field there at all: TestUploadsPageHeroIsAboutOwnFiles.
		if tool.Uploads {
			continue
		}
		t.Run(tool.Url, func(t *testing.T) {
			page := renderIndexMain(t, &tool)
			button := submitButton(t, page)
			if !strings.Contains(button, "home.open") || strings.Contains(button, "home.search") {
				t.Errorf("tool page button does not say Open: %s", button)
			}
			if !strings.Contains(button, `data-umami-event="search"`) {
				t.Errorf("event name changed: %s", button)
			}
			if !strings.Contains(button, `data-umami-event-page="`+tool.Url+`"`) {
				t.Errorf("no page prop: %s", button)
			}
			if strings.Contains(page, loupePath) {
				t.Error("tool page still shows the loupe")
			}
		})
	}
}

// The page about uploading one's own files (Tool.Uploads) opens with the drop
// zone alone: no magnet field, no "or", and no row of the demo magnet and the
// speed test. The zone says own files or a folder, not .torrent (a dropped
// .torrent still opens, lib/drop.js, it is just not offered), with the
// folder button inside it as on the home page. Every other page keeps all of
// it as it was.
func TestUploadsPageHeroIsAboutOwnFiles(t *testing.T) {
	torrentBits := []string{
		`placeholder="home.placeholder"`, `class="btn join-item btn-pink`, ">home.or<",
		`data-umami-event="try-demo"`, "magnet:?xt=urn:btih:demo", `data-umami-event="speedtest-hero"`,
		`>home.dropzoneUploads</span> home.dropzoneUploadsHint`,
	}
	const ownZone = `>home.dropzoneOwnFiles</span> home.dropzoneUploadsHint`
	pages := map[string]*hc.Tool{"home": nil}
	for i := range hc.Tools {
		pages[hc.Tools[i].Url] = &hc.Tools[i]
	}
	own := 0
	for name, tool := range pages {
		uploads := tool != nil && tool.Uploads
		if uploads {
			own++
		}
		t.Run(name, func(t *testing.T) {
			// Signed in: the guest's sign-in card needs the real context
			// (TestDropzoneUploads, which also reads the real copy).
			page := renderIndexMainWith(t, tool, template.FuncMap{
				"uploadsEnabled": func() bool { return true },
				"hasAuth":        func(any) bool { return true },
			})
			for _, s := range torrentBits {
				if got := strings.Contains(page, s); got == uploads {
					t.Errorf("%q rendered: %v, want %v", s, got, !uploads)
				}
			}
			if got := strings.Contains(page, ownZone); got != uploads || strings.Contains(page, "home.dropzoneOwnFiles") != uploads {
				t.Errorf("the own-files text of the zone rendered: %v, want %v", got, uploads)
			}
			i := strings.Index(page, `class="dropzone"`)
			if i < 0 {
				t.Fatalf("no drop zone:\n%s", page)
			}
			z := page[i : i+strings.Index(page[i:], "</form>")]
			if !strings.Contains(z, "dropzone-folder") || !strings.Contains(z, "upload.folder") {
				t.Errorf("no folder button inside the zone:\n%s", z)
			}
		})
	}
	if own == 0 {
		t.Fatal("no tool is marked Uploads — the test ran against nothing")
	}
}

// Elsewhere the hero's bottom padding and the first section's top padding are
// kept apart by the stats and trust strips. The page about one's own files has
// neither (TestUploadsPageHasNoTorrentBlocks), and the two paddings stacked
// into 152px of nothing under the drop zone, against 88px at most on a tool
// page. Its hero has no bottom padding of its own; every other hero keeps it.
func TestUploadsPageHeroLeavesNoGapToTheSteps(t *testing.T) {
	pages := map[string]*hc.Tool{"home": nil}
	for i := range hc.Tools {
		pages[hc.Tools[i].Url] = &hc.Tools[i]
	}
	own := 0
	for name, tool := range pages {
		uploads := tool != nil && tool.Uploads
		if uploads {
			own++
		}
		t.Run(name, func(t *testing.T) {
			page := renderIndexMain(t, tool)
			i := strings.Index(page, "<section ")
			if i < 0 {
				t.Fatal("no hero section")
			}
			hero := page[i : i+strings.Index(page[i:], ">")]
			if got := strings.Contains(hero, " pb-16 "); got == uploads {
				t.Errorf("the hero's bottom padding rendered: %v, want %v: %s", got, !uploads, hero)
			}
		})
	}
	if own == 0 {
		t.Fatal("no tool is marked Uploads — the test ran against nothing")
	}
}

// The error above the form quotes its numbers when it has them
// (error.hash_length: how many characters were pasted, how many a full
// infohash has) and is a plain message otherwise.
func TestIndexErrorQuotesItsNumbers(t *testing.T) {
	type errArgs struct{ Count, Full int }
	page := renderIndexMain(t, nil, map[string]interface{}{
		"ErrKey":  "error.hash_length",
		"ErrArgs": &errArgs{Count: 39, Full: 40},
	})
	if !strings.Contains(page, `<pre class="error">tn(error.hash_length,39,[Full 40])</pre>`) {
		t.Errorf("hash_length rendered without its numbers:\n%s", errorBlock(page))
	}
	page = renderIndexMain(t, nil, map[string]interface{}{"ErrKey": "error.free_text"})
	if !strings.Contains(page, `<pre class="error">error.free_text</pre>`) {
		t.Errorf("a plain message changed:\n%s", errorBlock(page))
	}
}

// The size badge under the drop zone: torrents have no size limit, own files
// have the account's upload cap. It names the cap only where uploads are on
// and there is a cap to name; a viewer without a paid plan reads the upsell.
// On the page about uploading one's own files (Tool.Uploads) it is the own
// files half alone, and with no cap to name there is no badge: "no size
// limit" would be said of the user's own files there.
func TestIndexBadgeSaysTheUploadCap(t *testing.T) {
	// It wraps on a phone: the upload texts run to 90 characters, and a badge
	// is one line high.
	const open = `<span class="badge badge-sm bg-white/5 border-0 text-w-muted mt-3 max-sm:h-auto max-sm:whitespace-normal max-sm:py-1">`
	other := hc.ToolByURL("magnet-to-torrent")
	own := hc.ToolByURL("private-cloud-media-server")
	if other == nil || own == nil || other.Uploads || !own.Uploads {
		t.Fatalf("the pages this test needs moved: %v, %v", other, own)
	}
	for _, tc := range []struct {
		name    string
		tool    *hc.Tool
		enabled bool
		cap     string
		paid    bool
		want    string // "" is no badge
	}{
		{"paid", nil, true, "50\u00a0GB", true, "home.sizeLimitUploads"},
		{"free", nil, true, "10\u00a0GB", false, "home.sizeLimitUploadsUpsell"},
		{"no claims", nil, true, "", true, "home.noSizeLimit"},
		{"uploads off", nil, false, "50\u00a0GB", true, "home.noSizeLimit"},
		{"tool page, paid", other, true, "50\u00a0GB", true, "home.sizeLimitUploads"},
		{"tool page, free", other, true, "10\u00a0GB", false, "home.sizeLimitUploadsUpsell"},
		{"tool page, no claims", other, true, "", true, "home.noSizeLimit"},
		{"uploads page, paid", own, true, "50\u00a0GB", true, "home.sizeLimitOwnFiles"},
		{"uploads page, free", own, true, "10\u00a0GB", false, "home.sizeLimitOwnFilesUpsell"},
		{"uploads page, no claims", own, true, "", true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page := renderIndexMainWith(t, tc.tool, template.FuncMap{
				"uploadsEnabled": func() bool { return tc.enabled },
				"uploadCap":      func(any) string { return tc.cap },
				"isPaid":         func(any) bool { return tc.paid },
				// Signed in: the guest's sign-in card is TestDropzoneUploads' subject.
				"hasAuth": func(any) bool { return true },
			})
			i := strings.Index(page, open)
			if tc.want == "" {
				if i >= 0 {
					t.Errorf("a badge with nothing to say: %s", page[i:i+strings.Index(page[i:], "</span>")])
				}
				return
			}
			if i < 0 {
				t.Fatal("no size badge under the drop zone")
			}
			i += len(open)
			if got := page[i : i+strings.Index(page[i:], "</span>")]; got != tc.want {
				t.Errorf("badge says %q, want %q", got, tc.want)
			}
		})
	}
}

func errorBlock(page string) string {
	if i := strings.Index(page, `id="index-error"`); i >= 0 {
		return page[i:min(len(page), i+300)]
	}
	return "(no error block)"
}

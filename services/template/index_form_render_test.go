package template

import (
	"bytes"
	"fmt"
	"html/template"
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
	}
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
	for _, name := range []string{"hero_backdrop", "hero_wave", "load/progress", "promo", "onboarding_checklist", "continue_watching", "discover", "about"} {
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

func errorBlock(page string) string {
	if i := strings.Index(page, `id="index-error"`); i >= 0 {
		return page[i:min(len(page), i+300)]
	}
	return "(no error block)"
}

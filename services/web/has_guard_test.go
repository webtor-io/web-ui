package web

import (
	"bytes"
	"html/template"
	"os"
	"regexp"
	"strings"
	"testing"
)

// Container ids are shared across page types (#list exists on both the resource
// page and the library page), so a stale X-Layout can run one page's snippet
// against the other page's data. A plain `{{ if .Field }}` does not survive
// that: evaluating a field the type does not have is a template error even
// inside an if, and it 500s the whole fragment.
//
// This pins the guard idiom used in partials/list.html and views/resource/get.html.
func TestHasGuardsMismatchedDataShapes(t *testing.T) {
	type resourceData struct{ List *struct{ N int } }
	type libraryData struct{ Items []string }

	h := &Helper{}
	tmpl := template.Must(template.New("t").Funcs(template.FuncMap{"has": h.Has}).
		Parse(`{{ if has . "List" }}listing{{ else }}nothing{{ end }}`))

	cases := map[string]struct {
		data any
		want string
	}{
		"right shape, populated": {&resourceData{List: &struct{ N int }{1}}, "listing"},
		"right shape, nil field": {&resourceData{}, "nothing"},
		"wrong shape entirely":   {&libraryData{Items: []string{"a"}}, "nothing"},
	}
	for name, c := range cases {
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, c.data); err != nil {
			t.Fatalf("%s: execute failed, the guard must not error: %v", name, err)
		}
		if buf.String() != c.want {
			t.Errorf("%s: want %q, got %q", name, c.want, buf.String())
		}
	}

	// And the unguarded form is what actually breaks — proving the guard earns
	// its keep rather than being defensive noise.
	bad := template.Must(template.New("b").Parse(`{{ if .List }}listing{{ end }}`))
	err := bad.Execute(&bytes.Buffer{}, &libraryData{})
	if err == nil {
		t.Fatal("expected the unguarded template to fail on a mismatched shape")
	}
	if !strings.Contains(err.Error(), "can't evaluate field List") {
		t.Errorf("unexpected error: %v", err)
	}
}

// The library-button snippet lives inside a data-async-layout attribute, so it
// is a literal at page-parse time and only becomes a template when the browser
// sends it back as X-Layout. Nothing else would catch a mistake in it, and it
// is exactly the snippet that produced
// "executing \"library/index\" at <.Data.Resource>: can't evaluate field
// Resource in type interface {}" in production.
func TestLibraryButtonLayoutSnippetSurvivesForeignData(t *testing.T) {
	raw, err := os.ReadFile("../../templates/views/resource/get.html")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile("data-async-layout=\"\\{\\{`(.*?library/button.*?)`\\}\\}\"").FindSubmatch(raw)
	if m == nil {
		t.Fatal("library-button layout snippet not found — did the attribute change?")
	}
	snippet := string(m[1])

	h := &Helper{}
	tmpl, err := template.New("snippet").Funcs(template.FuncMap{
		"has":         h.Has,
		"withContext": func(ctx any, data any) any { return map[string]any{"Ctx": ctx, "Data": data} },
		"template":    func(string, any) string { return "" },
	}).Parse(strings.Replace(snippet, `{{ template "library/button" (withContext $ .Resource) }}`, `BUTTON`, 1))
	if err != nil {
		t.Fatalf("snippet is not valid template syntax: %v", err)
	}

	// Pointer, matching handlers/resource.GetData.Resource — see
	// TestHasOnlySupportsNilableFields for why the field kind matters.
	type extendedResource struct{ ID string }
	type resourceCtx struct{ Data any }
	type resourceData struct{ Resource *extendedResource }
	type libraryData struct{ Items []string }

	for name, tc := range map[string]struct {
		data any
		want string
	}{
		"own page":     {&resourceData{Resource: &extendedResource{ID: "r"}}, "BUTTON"},
		"nil field":    {&resourceData{}, ""},
		"library page": {&libraryData{}, ""},
		"no data":      {nil, ""},
	} {
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, &resourceCtx{Data: tc.data}); err != nil {
			t.Errorf("%s: snippet must not error, got %v", name, err)
			continue
		}
		if strings.TrimSpace(buf.String()) != tc.want {
			t.Errorf("%s: want %q, got %q", name, tc.want, strings.TrimSpace(buf.String()))
		}
	}
}

// Has is implemented with reflect.Value.IsNil, which panics on kinds that
// cannot be nil. Guarding a plain string or struct field with it would trade a
// render error for a panic — worse. Pinned here so the limitation is not
// rediscovered the hard way.
func TestHasOnlySupportsNilableFields(t *testing.T) {
	h := &Helper{}
	tmpl := template.Must(template.New("t").Funcs(template.FuncMap{"has": h.Has}).
		Parse(`{{ if has . "Name" }}yes{{ end }}`))

	type nilable struct{ Name *string }
	if err := tmpl.Execute(&bytes.Buffer{}, &nilable{}); err != nil {
		t.Errorf("pointer field must be fine: %v", err)
	}

	type notNilable struct{ Name string }
	if err := tmpl.Execute(&bytes.Buffer{}, &notNilable{Name: "x"}); err == nil {
		t.Error("expected has to fail on a non-nilable field — if this ever passes, the limitation is gone and the comment above is stale")
	}
}

package template

import (
	"bytes"
	"fmt"
	"html/template"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/webtor-io/web-ui/services/offer"
)

type thCtx struct{}

func (thCtx) GetGinContext() *gin.Context { return nil }

// testHelper mirrors real helper method shapes: single return, variadic,
// safe-HTML return, int return, pointer arg.
type testHelper struct{}

func (testHelper) Greet(name string) string          { return "hi " + name }
func (testHelper) Safe() template.HTML               { return template.HTML("<b>x</b>") }
func (testHelper) Tp(key string, args ...any) string { return fmt.Sprintf("%s:%v", key, args) }
func (testHelper) Num(n int) int                     { return n * 2 }
func (testHelper) Deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func TestWithHelperBoundMethods(t *testing.T) {
	m := &Manager[thCtx]{funcs: FuncMap{}}
	m.WithHelper(testHelper{})

	for _, fn := range []string{"greet", "safe", "tp", "num", "deref"} {
		if _, ok := m.funcs[fn]; !ok {
			t.Fatalf("helper %q not registered", fn)
		}
	}

	tmpl := template.Must(template.New("t").Funcs(template.FuncMap(m.funcs)).Parse(
		`{{ greet "bob" }}|{{ safe }}|{{ tp "k" "a" "b" }}|{{ num 21 }}|{{ deref .P }}`))
	s := "v"
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]any{"P": &s}); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	got := buf.String()
	want := "hi bob|<b>x</b>|k:[a b]|42|v"
	if got != want {
		t.Fatalf("render mismatch:\n got=%q\nwant=%q", got, want)
	}
}

// trialURL is bound the way serve.go binds it (offer.Helper through
// WithHelper) and returns (string, error): the link renders, and an unknown
// surface stops the render instead of producing a link nobody can count.
func TestWithHelperBindsTrialURL(t *testing.T) {
	m := &Manager[thCtx]{funcs: FuncMap{}}
	m.WithHelper(offer.NewHelper(offer.New(nil, nil), nil))
	tmpl := template.Must(template.New("t").Funcs(template.FuncMap(m.funcs)).Parse(
		`<a href="{{ or (trialURL .Lang .From .O) "/donate" }}">`))
	trial := &offer.Offer{TrialDays: 7, URL: "https://checkout.example/?trial"}
	for _, c := range []struct {
		from string
		o    *offer.Offer
		want string
	}{
		{"grace", trial, `<a href="/ru/trial?from=grace">`},
		{"grace", &offer.Offer{URL: "https://checkout.example/"}, `<a href="/donate">`},
		{"grace", nil, `<a href="/donate">`},
	} {
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, map[string]any{"Lang": "ru", "From": c.from, "O": c.o}); err != nil {
			t.Fatalf("execute: %v", err)
		}
		if buf.String() != c.want {
			t.Errorf("%+v: %q, want %q", c, buf.String(), c.want)
		}
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]any{"Lang": "ru", "From": "reddit", "O": trial}); err == nil {
		t.Errorf("an unknown surface must fail the render, got %q", buf.String())
	}
}

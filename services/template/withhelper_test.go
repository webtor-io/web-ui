package template

import (
	"bytes"
	"fmt"
	"html/template"
	"testing"

	"github.com/gin-gonic/gin"
)

type thCtx struct{}

func (thCtx) GetGinContext() *gin.Context { return nil }

// testHelper mirrors real helper method shapes: single return, variadic,
// safe-HTML return, int return, pointer arg.
type testHelper struct{}

func (testHelper) Greet(name string) string          { return "hi " + name }
func (testHelper) Safe() template.HTML                { return template.HTML("<b>x</b>") }
func (testHelper) Tp(key string, args ...any) string  { return fmt.Sprintf("%s:%v", key, args) }
func (testHelper) Num(n int) int                      { return n * 2 }
func (testHelper) Deref(p *string) string             { if p == nil { return "" }; return *p }

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

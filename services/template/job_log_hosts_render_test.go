package template_test

import (
	"bytes"
	"html/template"
	"strings"
	"testing"

	"github.com/webtor-io/web-ui/services/web"
)

// Every job log host but the embed's renders partials/job_log.html: the
// stage line, the notes and verdicts, the folded steps (lib/progressLog.js
// looks for .job-stage to draw it). A host that went back to a bare
// .log-target would still work -- as the old monospace console.
func TestJobLogHostsRenderTheStage(t *testing.T) {
	funcs := template.FuncMap{
		"t":              func(lang, key string, args ...interface{}) string { return key },
		"tpHTML":         func(lang, key string, args ...interface{}) template.HTML { return template.HTML(key) },
		"langPath":       func(lang, p string) string { return p },
		"asset":          func(p string) template.HTML { return template.HTML(p) },
		"makeJobLogURL":  func(lang string, j interface{}) string { return "/log" },
		"useSuperTokens": func() bool { return true },
		"has":            (&web.Helper{}).Has,
	}
	type actionData struct{ Job interface{} }
	type payment struct{ InvoiceURL string }
	hosts := []struct {
		file, define string
		data         interface{}
	}{
		{"views/action/post.html", "main", map[string]interface{}{"Lang": "en", "Data": &actionData{Job: struct{}{}}}},
		{"views/donate/crypto_success.html", "main", map[string]interface{}{"Lang": "en", "Data": map[string]interface{}{"Pending": true, "Job": struct{}{}, "Payment": &payment{}}}},
		{"views/auth/login.html", "main", map[string]interface{}{"Lang": "en", "Data": map[string]interface{}{}}},
		{"views/auth/logout.html", "main", map[string]interface{}{"Lang": "en"}},
		{"partials/auth/form.html", "auth/form", map[string]interface{}{"Ctx": map[string]interface{}{"Lang": "en"}, "Data": map[string]interface{}{"ReturnURL": ""}}},
	}
	for _, h := range hosts {
		tpl, err := template.New(h.file).Funcs(funcs).ParseFiles("../../templates/"+h.file, "../../templates/partials/job_log.html")
		if err != nil {
			t.Fatalf("%s: parse: %v", h.file, err)
		}
		var buf bytes.Buffer
		if err := tpl.ExecuteTemplate(&buf, h.define, h.data); err != nil {
			t.Fatalf("%s: render: %v", h.file, err)
		}
		out := buf.String()
		for _, w := range []string{`class="job-stage"`, `class="job-results"`, `<div class="log-target">`, "action.showSteps", "data-job-announce"} {
			if !strings.Contains(out, w) {
				t.Errorf("%s: missing %q", h.file, w)
			}
		}
		if n := strings.Count(out, `class="log-target"`); n != 1 {
			t.Errorf("%s: %d log targets, want one", h.file, n)
		}
	}
}

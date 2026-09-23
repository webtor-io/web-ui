package index

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-contrib/multitemplate"
	"github.com/gin-gonic/gin"

	"github.com/webtor-io/web-ui/handlers/common"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/web"
)

// newHomeRouter serves the index routes with a stand-in index view that
// prints what the real one branches on, so the test reads status, headers
// and the error key without the whole template set. The template manager
// resolves "templates/" against the working directory, hence the chdir.
func newHomeRouter(t *testing.T) *gin.Engine {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"templates/layouts/main.html": `{{ template "main" . }}`,
		"templates/views/index.html":  `{{ define "main" }}[err={{ .ErrKey }}][instruction={{ .Data.Instruction }}]{{ end }}`,
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "templates/partials"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(web.NoindexDefault(false))
	re := multitemplate.NewRenderer()
	r.HTMLRender = re
	tm := template.NewManager[*web.Context](re)
	RegisterHandler(r, tm, nil)
	if err := tm.Init(); err != nil {
		t.Fatal(err)
	}
	return r
}

func get(r *gin.Engine, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
	return w
}

// The home page and the tool pages come back with ?err= after a failed form
// (web.RedirectWithErrorAndPath) and, until now, after every dead resource
// link. That URL is one visit's state: it must not be indexed, while the
// page itself stays indexable.
func TestHomeWithErrIsNoindex(t *testing.T) {
	r := newHomeRouter(t)
	tool := "/" + common.Tools[0].Url

	cases := []struct {
		target    string
		robots    string
		wantInBod string
	}{
		{"/", "index, follow", "[err=]"},
		{tool, "index, follow", "[err=]"},
		{"/?status=error&err=error.not_found&from=%2Fdeadbeef", "noindex", "[err=error.not_found]"},
		{tool + "?status=error&err=error.invalid_resource&from=%2F", "noindex", "[err=error.invalid_resource]"},
		// Not shown without status=error, but still an error-state URL.
		{"/?err=error.not_found", "noindex", "[err=]"},
		{"/?status=success&from=%2Fprofile", "index, follow", "[err=]"},
	}
	for _, tc := range cases {
		w := get(r, tc.target)
		if w.Code != http.StatusOK {
			t.Errorf("%s: status %d, want 200", tc.target, w.Code)
		}
		if got := w.Header().Get("X-Robots-Tag"); got != tc.robots {
			t.Errorf("%s: X-Robots-Tag %q, want %q", tc.target, got, tc.robots)
		}
		if !strings.Contains(w.Body.String(), tc.wantInBod) {
			t.Errorf("%s: body %q, want it to contain %q", tc.target, w.Body.String(), tc.wantInBod)
		}
	}
}

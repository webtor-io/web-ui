package legal

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-contrib/multitemplate"
	"github.com/gin-gonic/gin"

	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/web"
)

// newLegalRouter serves the legal routes over stand-in views, registered the
// way serve.go registers them: the error views first (the centralized error
// handler owns them), then this handler. The template manager resolves
// "templates/" against the working directory, hence the chdir.
func newLegalRouter(t *testing.T) *gin.Engine {
	t.Helper()
	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = locales.Close() })
	svc := i18n.New(locales.FS())

	dir := t.TempDir()
	files := map[string]string{
		"templates/layouts/main.html":     `{{ template "main" . }}`,
		"templates/views/legal/tos.html":  `{{ define "main" }}[tos]{{ end }}`,
		"templates/views/legal/dmca.html": `{{ define "main" }}[dmca]{{ end }}`,
		"templates/views/error/page.html": `{{ define "main" }}[error][err={{ .ErrKey }}]{{ end }}`,
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
	r.Use(i18n.GinMiddleware(svc))
	re := multitemplate.NewRenderer()
	r.HTMLRender = re
	tm := template.NewManager[*web.Context](re)
	tm.MustRegisterViews("error/*")
	RegisterHandler(r, tm)
	if err := tm.Init(); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestLegalPages(t *testing.T) {
	r := newLegalRouter(t)

	cases := []struct {
		path, lang string
		status     int
		body       string
		location   string
	}{
		{path: "/legal/tos", status: http.StatusOK, body: "[tos]"},
		{path: "/legal/dmca", status: http.StatusOK, body: "[dmca]"},
		// A name that gets requested; the page has never had it.
		{path: "/legal/terms", status: http.StatusMovedPermanently, location: "/legal/tos"},
		// The i18n middleware has stripped /ru and set the language.
		{path: "/legal/terms", lang: "ru", status: http.StatusMovedPermanently, location: "/ru/legal/tos"},
		// Unknown names were a bare 500 from the template manager.
		{path: "/legal/nope", status: http.StatusNotFound, body: "[error][err=error.page_not_found]"},
		{path: "/legal/", status: http.StatusNotFound, body: "[error][err=error.page_not_found]"},
		{path: "/legal/tos/", status: http.StatusNotFound, body: "[error][err=error.page_not_found]"},
		{path: "/legal/../index", status: http.StatusNotFound, body: "[error][err=error.page_not_found]"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		if tc.lang != "" {
			req.Header.Set(i18n.LangHeader, tc.lang)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		name := tc.path + " " + tc.lang
		if w.Code != tc.status {
			t.Errorf("%s: status %d, want %d", name, w.Code, tc.status)
		}
		if tc.body != "" && !strings.Contains(w.Body.String(), tc.body) {
			t.Errorf("%s: body %q, want %q", name, w.Body.String(), tc.body)
		}
		if got := w.Header().Get("Location"); got != tc.location {
			t.Errorf("%s: Location %q, want %q", name, got, tc.location)
		}
	}
}

// The 404 shows error.page_not_found: a key that is missing from a locale
// renders as the key itself.
func TestLegalNotFoundKeyIsTranslated(t *testing.T) {
	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = locales.Close() }()
	svc := i18n.New(locales.FS())
	for _, lang := range i18n.SupportedLangs {
		got := i18n.TranslateWithLocalizer(svc.Localizer(lang), "error.page_not_found")
		if got == "" || got == "error.page_not_found" {
			t.Errorf("%s: error.page_not_found is not translated (%q)", lang, got)
		}
	}
}

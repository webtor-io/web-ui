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
	// serve.go registers the resource handler after this one; its catch-all
	// is what a legal URL without a route of its own falls into.
	r.GET("/:resource_id", func(c *gin.Context) { c.String(http.StatusNotFound, "[resource]") })
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
		// The bare section names the terms; the resource catch-all used to
		// take it for a torrent id.
		{path: "/legal", status: http.StatusMovedPermanently, location: "/legal/tos"},
		{path: "/legal", lang: "ru", status: http.StatusMovedPermanently, location: "/ru/legal/tos"},
		// Unknown names were a bare 500 from the template manager.
		{path: "/legal/nope", status: http.StatusNotFound, body: "[error][err=error.page_not_found]"},
		{path: "/legal/", status: http.StatusNotFound, body: "[error][err=error.page_not_found]"},
		{path: "/legal/tos/", status: http.StatusNotFound, body: "[error][err=error.page_not_found]"},
		{path: "/legal/../index", status: http.StatusNotFound, body: "[error][err=error.page_not_found]"},
		// ...and the catch-all still gets everything else.
		{path: "/legals", status: http.StatusNotFound, body: "[resource]"},
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

// The DMCA page states what Webtor does with content, and a rights holder
// reads it against the rest of the site. "It does not index or search for
// content" was contradicted by the release subscriptions, which query the
// indexers and addons a user connected; the page now says what is true: no
// index or catalogue of Webtor's own, content from users and from the
// sources they connected, cached temporarily, kept longer only in Vault.
func TestDMCAStatesWhatWebtorDoes(t *testing.T) {
	b, err := os.ReadFile("../../templates/views/legal/dmca.html")
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if strings.Contains(text, "does not index or search") {
		t.Error("dmca.html still says Webtor does not search for content")
	}
	for _, want := range []string{"index or catalogue of content of its own", "Stremio addons", "Torznab indexers", "temporarily", "Vault"} {
		if !strings.Contains(text, want) {
			t.Errorf("dmca.html does not say %q", want)
		}
	}
}

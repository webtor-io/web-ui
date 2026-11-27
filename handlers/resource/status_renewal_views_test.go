package resource

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-contrib/multitemplate"
	"github.com/gin-gonic/gin"

	"github.com/webtor-io/web-ui/handlers/index"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/web"
)

// The transfer status renews itself by fetching its page again with the
// X-Layout of #torrent-status (lib/statusStream.js). When access is gone by
// then, the answer is another view: the private refusal (403), the home page
// (the redirect to / or /?err=, the 404 of a resource that names nothing) or
// the error page (a middleware that failed). A view without that template
// turned the renewal into a 500, "no such template". The real views and
// locales, through the manager's own X-Layout path.
func TestStatusRenewalOnAnotherView(t *testing.T) {
	get, err := os.ReadFile("../../templates/views/resource/get.html")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile("id=\"torrent-status\"[^>]*data-async-layout=\"\\{\\{`(.*?)`\\}\\}\"").FindSubmatch(get)
	if m == nil {
		t.Fatal("the data-async-layout of #torrent-status not found -- did the attribute change?")
	}
	layout := string(m[1])

	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { locales.Close() })
	tr := i18n.NewHelper(i18n.New(locales.FS()))

	dir := t.TempDir()
	files := map[string]string{"layouts/main.html": `{{ template "main" . }}`}
	for _, f := range []string{"views/index.html", "views/resource/private.html", "views/error/page.html", "partials/private/refusal.html"} {
		b, err := os.ReadFile("../../templates/" + f)
		if err != nil {
			t.Fatal(err)
		}
		files[f] = string(b)
	}
	for name, body := range files {
		p := filepath.Join(dir, "templates", name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)

	gin.SetMode(gin.TestMode)
	stub := func(...any) any { return nil }
	tm := template.NewManager[*web.Context](multitemplate.NewRenderer()).
		WithHelper(tr).
		WithFuncs(template.FuncMap{
			"langPath": func(_, p string) string { return p },
			"asset":    func(string) template.HTML { return "" },
			// Uploads off: the drop zone takes torrents only.
			"uploadsEnabled": func() bool { return false },
			// Parsed, never run by the status slot.
			"canonicalURL": stub, "demoMagnet": stub, "domain": stub, "has": stub,
			"isPaid": stub, "promoOffer": stub, "seoFriendly": stub, "withContext": stub,
			"bitsForHumans": stub, "hasAuth": stub, "uploadCap": stub, "uploadSignIn": stub,
		})
	tm.MustRegisterViews("*")
	tm.MustRegisterViews("resource/*")
	tm.MustRegisterViews("error/*")
	if err := tm.Init(); err != nil {
		t.Fatal(err)
	}

	refusal := web.NewPrivateRefusal(&api.PrivateError{Status: http.StatusForbidden, Code: api.CodePrivateTorrent, Reason: api.ReasonSignIn}, false, "")
	refusal.Target = "main"
	home := func(key string, args *web.ErrArgs) func(*web.Context) *web.Context {
		return func(c *web.Context) *web.Context {
			return c.WithData(&index.Data{}).WithErrKey(key).WithErrArgs(args)
		}
	}
	for _, tc := range []struct {
		name, view string
		status     int
		ctx        func(*web.Context) *web.Context
		want       string
	}{
		{"the redirect to /", "index", http.StatusOK, home("", nil), "Status unavailable"},
		{"a ban, redirected to /?err=", "index", http.StatusOK, home("error.forbidden", nil), "blocked at the request of the rights holder"},
		{"a message with numbers", "index", http.StatusOK, home("error.hash_length", &web.ErrArgs{Count: 39, Full: 40}), "it has 39 characters — a full one has 40"},
		{"a resource that names nothing", "index", http.StatusNotFound, home("error.not_found", nil), "Resource not found."},
		{"a private torrent refused", "resource/private", http.StatusForbidden,
			func(c *web.Context) *web.Context { return c.WithData(refusal) }, "This is a private torrent: sign in."},
		{"a middleware that failed", "error/page", http.StatusServiceUnavailable,
			func(c *web.Context) *web.Context { return c.WithErrKey("error.service_unavailable") }, "Please come back later."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodGet, "/"+privHash, nil)
			c.Request.Header.Set("X-Requested-With", "XMLHttpRequest")
			c.Request.Header.Set("X-Layout", layout)
			tm.Build(tc.view).WithLayout("main").HTML(tc.status, tc.ctx(web.NewContext(c)))
			body := rec.Body.String()
			if rec.Code != tc.status {
				t.Fatalf("status %d, want %d; body %q", rec.Code, tc.status, body)
			}
			if !strings.HasPrefix(body, `<template data-async-fragment="main">`) || !strings.Contains(body, tc.want) {
				t.Errorf("body %q, want the main fragment with %q", body, tc.want)
			}
		})
	}
}

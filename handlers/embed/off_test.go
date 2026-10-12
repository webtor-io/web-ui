package embed

import (
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-contrib/multitemplate"
	"github.com/gin-gonic/gin"
	"github.com/urfave/cli"

	"github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/embed"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/web"
)

// newEmbedRouter mounts the embed handler over stand-in views the way
// serve.go does, followed by the resource catch-all that /embed fell into
// when DISABLE_EMBED left it unregistered.
func newEmbedRouter(t *testing.T, args ...string) *gin.Engine {
	t.Helper()
	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = locales.Close() })
	svc := i18n.New(locales.FS())
	dir := t.TempDir()
	for name, body := range map[string]string{
		"templates/views/embed/get.html":  `[get]`,
		"templates/views/embed/post.html": `[post]`,
		"templates/views/embed/off.html":  `[off:{{ .Lang }}]`,
	} {
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

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range embed.RegisterFlags(nil) {
		f.Apply(fs)
	}
	cli.BoolFlag{Name: common.DisableEmbedFlag}.Apply(fs)
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(i18n.GinMiddleware(svc))
	re := multitemplate.NewRenderer()
	r.HTMLRender = re
	tm := template.NewManager[*web.Context](re)
	RegisterHandler(cli.NewContext(nil, fs, nil), nil, r, tm, nil, nil, nil)
	r.GET("/:resource_id", func(c *gin.Context) { c.String(http.StatusNotFound, "[resource]") })
	if err := tm.Init(); err != nil {
		t.Fatal(err)
	}
	return r
}

// With embeds off, a frame already on another site still gets an answer of
// ours -- the off page -- on the placeholder's GET and on a stale
// placeholder's POST, under either switch.
func TestEmbedOff(t *testing.T) {
	for _, args := range [][]string{{"--use-embed=false"}, {"--disable-embed"}} {
		t.Run(args[0], func(t *testing.T) {
			r := newEmbedRouter(t, args...)
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				w := httptest.NewRecorder()
				req := httptest.NewRequest(method, "/embed?id=x", strings.NewReader("settings={}"))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				r.ServeHTTP(w, req)
				if w.Code != http.StatusOK || w.Body.String() != "[off:en]" {
					t.Errorf("%s: %d %q, want 200 [off:en]", method, w.Code, w.Body.String())
				}
			}
		})
	}
}

func TestEmbedOnServesThePlaceholder(t *testing.T) {
	r := newEmbedRouter(t)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/embed?id=x", nil))
	if w.Body.String() != "[get]" {
		t.Errorf("GET /embed: %q, want [get]", w.Body.String())
	}
}

// The frame on another site has no language prefix and no lang cookie (it
// is SameSite=Lax), so the off page speaks the browser's language -- unless
// the request was routed to one.
func TestEmbedOffSpeaksTheBrowsersLanguage(t *testing.T) {
	r := newEmbedRouter(t, "--use-embed=false")
	for _, c := range []struct {
		accept, routed, want string
	}{
		{"ru-RU,ru;q=0.9,en;q=0.8", "", "[off:ru]"},
		{"en-US,en", "", "[off:en]"},
		{"xx", "", "[off:en]"},
		{"ru", "de", "[off:de]"}, // /de/embed
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/embed?id=x", nil)
		req.Header.Set("Accept-Language", c.accept)
		if c.routed != "" {
			req.Header.Set(i18n.LangHeader, c.routed)
		}
		r.ServeHTTP(w, req)
		if w.Body.String() != c.want {
			t.Errorf("Accept-Language %q, routed %q: %q, want %q", c.accept, c.routed, w.Body.String(), c.want)
		}
		if w.Header().Get("Vary") != "Accept-Language" {
			t.Errorf("Accept-Language %q: no Vary: Accept-Language", c.accept)
		}
	}
}

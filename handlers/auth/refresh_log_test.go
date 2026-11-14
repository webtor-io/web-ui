package auth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-contrib/multitemplate"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus/hooks/test"

	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/web"
)

// The interstitial is logged with the page it stands in for. Through the
// browser's magnet: handler (manifest "/%s") that page's path is the whole
// magnet, trackers included, and a private tracker's passkey is part of a
// tracker URL.
func TestRefresh_LogKeepsNoMagnetTrackers(t *testing.T) {
	const passkey = "0123456789abcdef0123456789abcdef"
	dir := t.TempDir()
	for name, body := range map[string]string{
		"templates/layouts/main.html":       `{{ template "main" . }}`,
		"templates/views/auth/refresh.html": `{{ define "main" }}refresh{{ end }}`,
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
	gin.SetMode(gin.TestMode)
	re := multitemplate.NewRenderer()
	tm := template.NewManager[*web.Context](re)
	h := &Handler{tb: tm.MustRegisterViews("auth/*").WithLayout("main")}
	if err := tm.Init(); err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.HTMLRender = re
	r.GET("/:resource_id", h.refresh)

	hook := test.NewGlobal()
	defer hook.Reset()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet,
		"/magnet%3A%3Fxt%3Durn%3Abtih%3A08ada5a7a6183aae1e09d831df6748d566095a10%26tr%3Dhttp%253A%252F%252Ftracker.invalid%252Fannounce%253Fpasskey%253D"+passkey, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var lines []string
	for _, e := range hook.AllEntries() {
		lines = append(lines, e.Message+fmt.Sprint(e.Data))
	}
	all := strings.Join(lines, "\n")
	if strings.Contains(all, passkey) || strings.Contains(all, "tracker.invalid") {
		t.Errorf("the log keeps the tracker:\n%s", all)
	}
	if !strings.Contains(all, "session refresh interstitial") || !strings.Contains(all, "tr=<redacted>") {
		t.Errorf("the line lost its page:\n%s", all)
	}
}

package action

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gin-contrib/multitemplate"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/turnstile"
	"github.com/webtor-io/web-ui/services/web"
)

type fakeVerifier struct {
	token, ip string
	err       error
	calls     int
}

func (f *fakeVerifier) Validate(token, ip string) error {
	f.calls++
	f.token, f.ip = token, ip
	return f.err
}

func postCtx(form url.Values, headers map[string]string) *gin.Context {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/stream-video", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	c.Request = req
	return c
}

func TestVerifyActionAnonymous(t *testing.T) {
	fv := &fakeVerifier{}
	h := &Handler{verifier: fv}
	c := postCtx(url.Values{"cf-turnstile-response": {"tok-1"}}, map[string]string{"CF-Connecting-IP": "203.0.113.9"})
	if err := h.verifyAction(c); err != nil {
		t.Fatalf("valid token refused: %v", err)
	}
	if fv.calls != 1 || fv.token != "tok-1" || fv.ip != "203.0.113.9" {
		t.Fatalf("verifier calls=%d token=%q ip=%q", fv.calls, fv.token, fv.ip)
	}
	fv.err = errors.New("turnstile verification failed")
	if err := h.verifyAction(postCtx(url.Values{}, nil)); err == nil {
		t.Fatal("missing token must be refused when a verifier is configured")
	}
}

// No verifier configured (nil, or the typed nil the constructor returns
// when the keys are absent): nothing is checked, the gate does not exist.
func TestVerifyActionUnconfigured(t *testing.T) {
	for _, h := range []*Handler{{verifier: nil}, {verifier: (*turnstile.Service)(nil)}} {
		if err := h.verifyAction(postCtx(url.Values{}, nil)); err != nil {
			t.Fatalf("unconfigured verifier must not refuse: %v", err)
		}
	}
}

// Every job-start route RegisterHandler mounts refuses an anonymous start
// without a token before any job exists (jobs is nil here: reaching it would
// panic). A new start that skipped h.post would be a way around the check.
// The client half -- which forms get a token -- is lib/turnstileAction.js,
// whose list must name the same routes.
func TestJobStartRoutesRequireTurnstile(t *testing.T) {
	dir := t.TempDir()
	post := filepath.Join(dir, "templates/views/action/post.html")
	if err := os.MkdirAll(filepath.Dir(post), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "templates/partials"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(post, []byte(`{{ define "main" }}[refused:{{ .ErrKey }}]{{ end }}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(sessions.Sessions("session", cookie.NewStore([]byte("test secret"))))
	re := multitemplate.NewRenderer()
	r.HTMLRender = re
	tm := template.NewManager[*web.Context](re)
	fv := &fakeVerifier{err: errors.New("missing-input-response")}
	RegisterHandler(r, tm, nil, nil, fv, nil)
	if err := tm.Init(); err != nil {
		t.Fatal(err)
	}
	routes := []string{"/download-file", "/download-dir", "/preview-image", "/stream-audio", "/stream-video", "/file-info"}
	for _, path := range routes {
		calls := fv.calls
		form := url.Values{"resource-id": {"r"}, "item-id": {"i"}}
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest || fv.calls != calls+1 {
			t.Errorf("%s: status %d, verifier calls %d: an anonymous start without a token must be refused", path, w.Code, fv.calls-calls)
		}
	}
	// And nothing else mounted here starts a job unchecked.
	for _, ri := range r.Routes() {
		if ri.Method == http.MethodPost && !slices.Contains(routes, ri.Path) {
			t.Errorf("POST %s is not in the list above: a job start must be checked here and in lib/turnstileAction.js", ri.Path)
		}
	}
}

package action

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/services/turnstile"
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

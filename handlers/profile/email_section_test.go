package profile

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newEmailRouter(h *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/profile/email", h.setEmail)
	return r
}

func postEmail(r *gin.Engine, email string) *httptest.ResponseRecorder {
	form := url.Values{"email": {email}}
	req := httptest.NewRequest(http.MethodPost, "/profile/email", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestSetEmailRejectsUndeliverableAddress is task 7's first rule: an
// undeliverable address must be rejected through the existing form-error
// mechanism, and nothing may be stored.
//
// The handler is deliberately given a zero-value Handler, in particular a
// nil *cs.PG: setEmail must reject the address and return before it ever
// calls s.pg.Get(), which panics on a nil receiver (cs.PG.Get locks a mutex
// field before checking anything else). So this test does not merely check
// the redirect -- if the deliverability check were skipped, weakened, or
// moved after the DB call, this test would panic rather than fail a soft
// assertion, which is a strictly stronger signal that nothing reached
// storage.
func TestSetEmailRejectsUndeliverableAddress(t *testing.T) {
	h := &Handler{}
	r := newEmailRouter(h)

	w := postEmail(r, "not-an-email")

	if !strings.Contains(w.Header().Get("Location"), "err=profile.email.invalid") {
		t.Errorf("undeliverable address was not reported as a failure; redirected to %q", w.Header().Get("Location"))
	}
}

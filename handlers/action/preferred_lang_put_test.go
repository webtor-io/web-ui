package action

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
)

// accountStore records what the handler asked the profile to keep.
type accountStore struct{ calls []string }

func (a *accountStore) SetPreferredLang(_ context.Context, _ *auth.User, code string) error {
	a.calls = append(a.calls, code)
	return nil
}

func preferredLangEngine(store PreferredLangStore) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(sessions.Sessions("session", cookie.NewStore([]byte("test secret"))))
	h := &Handler{prefs: store}
	r.PUT("/stream-video/preferred-lang", h.putPreferredLang)
	r.GET("/read", func(c *gin.Context) {
		ud := models.NewVideoStreamUserData("res", "item", &models.StreamSettings{})
		ud.FetchSessionData(c)
		c.JSON(http.StatusOK, gin.H{"lang": ud.PreferredLang})
	})
	return r
}

func putLang(t *testing.T, r *gin.Engine, body string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/stream-video/preferred-lang", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func readLang(t *testing.T, r *gin.Engine, cookies []*http.Cookie) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/read", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var out struct{ Lang string }
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("read: %v", err)
	}
	return out.Lang
}

// A viewer without an account keeps the language in the session, where the
// stream job reads it; the profile store is never asked.
func TestPutPreferredLangAnonymousKeepsItInTheSession(t *testing.T) {
	store := &accountStore{}
	r := preferredLangEngine(store)

	w := putLang(t, r, `{"lang":"kk"}`, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status %d", w.Code)
	}
	cookies := w.Result().Cookies()
	if got := readLang(t, r, cookies); got != "kk" {
		t.Fatalf("session language = %q, want kk", got)
	}
	if len(store.calls) != 0 {
		t.Fatalf("an anonymous viewer has no profile to write: %v", store.calls)
	}

	// "" clears it: the browser decides again.
	w = putLang(t, r, `{"lang":""}`, cookies)
	if w.Code != http.StatusNoContent {
		t.Fatalf("clear: status %d", w.Code)
	}
	if got := readLang(t, r, w.Result().Cookies()); got != "" {
		t.Fatalf("cleared language reads %q", got)
	}
}

// Only languages the platform knows: the value ends up in a job cache key
// and in the ladder, and anything else would silently resolve to the
// browser's language while the select shows what was asked for.
func TestPutPreferredLangRefusesUnknownCodes(t *testing.T) {
	r := preferredLangEngine(&accountStore{})
	for _, body := range []string{`{"lang":"tlh"}`, `{"lang":"../etc"}`, `{"lang":"english"}`} {
		w := putLang(t, r, body, nil)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: status %d, want 400", body, w.Code)
		}
		if got := readLang(t, r, w.Result().Cookies()); got != "" {
			t.Fatalf("%s: stored %q", body, got)
		}
	}
}

func TestPreferredLangCode(t *testing.T) {
	for in, want := range map[string]string{"": "", " kk ": "kk", "pt": "pt"} {
		got, err := preferredLangCode(in)
		if err != nil || got != want {
			t.Fatalf("preferredLangCode(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := preferredLangCode("xx"); err == nil {
		t.Fatal("an unknown code must be refused")
	}
}

package api

import (
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-contrib/sessions/redis"
	"github.com/gin-gonic/gin"
)

// sidEngine serves the anonymous session id the way the app computes it:
// the store, the embed's handed-in id (handlers/session), the CSRF salt
// saved into a new session before the claims are made, then the handler,
// which saves the session again -- as picking an audio track or a language
// does.
func sidEngine(store sessions.Store) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(sessions.Sessions("session", store))
	r.Use(func(c *gin.Context) {
		if id := c.GetHeader("X-Session-ID"); id != "" {
			c.Request.AddCookie(&http.Cookie{Name: "session", Value: id})
		}
	})
	r.Use(func(c *gin.Context) {
		s := sessions.Default(c)
		if s.Get("salt") == nil {
			s.Set("salt", "x")
			_ = s.Save()
		}
	})
	r.GET("/", func(c *gin.Context) {
		sid := GenerateSessionID(c)
		s := sessions.Default(c)
		s.Set("audio", time.Now().UnixNano())
		_ = s.Save()
		c.Header("X-Sid", sid)
		c.Header("X-Store-Id", s.ID())
	})
	return r
}

func sidGet(r *gin.Engine, cookie, handedIn string) (sid, storeID, setCookie string) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "session", Value: cookie})
	}
	if handedIn != "" {
		req.Header.Set("X-Session-ID", handedIn)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	for _, c := range w.Result().Cookies() {
		if c.Name == "session" {
			setCookie = c.Value
		}
	}
	return w.Header().Get("X-Sid"), w.Header().Get("X-Store-Id"), setCookie
}

func sha1hex(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

// An anonymous viewer's id was the hash of the session cookie, which the
// store encodes anew, with the time, on every save: after an autoplay's
// track pick the status stream reopened under another id than the
// player's tokens, and the viewer's link stayed empty for the rest of the
// episode. It is the hash of the session's id in the store, the same across
// saves -- and the same for the embed, which hands that id in raw.
func TestGenerateSessionID_StableAcrossSaves(t *testing.T) {
	mr := miniredis.RunT(t)
	store, err := redis.NewStore(2, "tcp", mr.Addr(), "", []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	r := sidEngine(store)
	_, id, c1 := sidGet(r, "", "")
	if c1 == "" || id == "" {
		t.Fatalf("first visit: cookie %q id %q", c1, id)
	}
	// securecookie stamps whole seconds: the next save's cookie differs
	// only a second later.
	time.Sleep(1100 * time.Millisecond)
	sid2, _, c2 := sidGet(r, c1, "")
	if c2 == c1 {
		t.Fatal("the cookie did not change across saves: nothing to test")
	}
	sid3, id3, _ := sidGet(r, c2, "")
	if sid2 == "" || sid2 != sid3 || id3 != id {
		t.Errorf("sid %q then %q across saves (store id %q, %q)", sid2, sid3, id, id3)
	}
	if sid2 != sha1hex(id) {
		t.Errorf("sid %q, want the hash of the store id", sid2)
	}
	// The embed without a cookie (third-party cookies blocked) hands the
	// page's window._sessionID in: the store id itself.
	if sidE, _, _ := sidGet(r, "", id); sidE != sid2 {
		t.Errorf("handed-in id: sid %q, the page's %q", sidE, sid2)
	}
	if sid, _, _ := sidGet(r, "", ""); sid != "" {
		t.Errorf("no cookie: sid %q, want none", sid)
	}
}

// The cookie store keeps no id: the sid stays the hash of the cookie.
func TestGenerateSessionID_CookieStoreKeepsTheCookie(t *testing.T) {
	r := sidEngine(cookie.NewStore([]byte("secret")))
	_, id, c1 := sidGet(r, "", "")
	if id != "" {
		t.Fatalf("the cookie store has an id: %q", id)
	}
	if sid, _, _ := sidGet(r, c1, ""); sid != sha1hex(c1) {
		t.Errorf("sid %q, want the hash of the cookie", sid)
	}
}

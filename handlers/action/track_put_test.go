package action

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/models"
)

// trackChoiceEngine wires the two PUT handlers behind the same session
// middleware the app uses, plus a read-back route, so the test exercises the
// real cookie plumbing rather than a stub session.
func trackChoiceEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(sessions.Sessions("session", cookie.NewStore([]byte("test secret"))))
	r.PUT("/stream-video/subtitle", func(c *gin.Context) {
		if err := putTrackChoice(c, trackSubtitle); err != nil {
			c.AbortWithStatus(http.StatusBadRequest)
		}
	})
	r.PUT("/stream-video/audio", func(c *gin.Context) {
		if err := putTrackChoice(c, trackAudio); err != nil {
			c.AbortWithStatus(http.StatusBadRequest)
		}
	})
	r.GET("/read", func(c *gin.Context) {
		ud := models.NewVideoStreamUserData("res", "item", &models.StreamSettings{})
		ud.FetchSessionData(c)
		c.JSON(http.StatusOK, gin.H{"subtitle": ud.SubtitleID, "audio": ud.AudioID})
	})
	return r
}

// session carries the cookie jar between requests by hand: the choices live
// in the session, so a test that dropped the cookie would read an empty one
// back and pass no matter what the handlers did.
type trackSession struct {
	t      *testing.T
	r      *gin.Engine
	cookie string
}

func newTrackSession(t *testing.T) *trackSession {
	t.Helper()
	return &trackSession{t: t, r: trackChoiceEngine()}
}

func (s *trackSession) do(method, path, body string) *httptest.ResponseRecorder {
	s.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if s.cookie != "" {
		req.Header.Set("Cookie", s.cookie)
	}
	w := httptest.NewRecorder()
	s.r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		s.t.Fatalf("%s %s: status %d", method, path, w.Code)
	}
	if set := w.Result().Header.Get("Set-Cookie"); set != "" {
		s.cookie = strings.Split(set, ";")[0]
	}
	return w
}

func (s *trackSession) put(kind, id string) {
	s.t.Helper()
	s.do(http.MethodPut, "/stream-video/"+kind,
		`{"id":"`+id+`","resourceID":"res","itemID":"item"}`)
}

func (s *trackSession) read() (subtitle, audio string) {
	s.t.Helper()
	w := s.do(http.MethodGet, "/read", "")
	var out struct {
		Subtitle string `json:"subtitle"`
		Audio    string `json:"audio"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		s.t.Fatalf("read: %v", err)
	}
	return out.Subtitle, out.Audio
}

// The two ids are independent choices, and neither PUT may cancel the
// other's. Both orders, because the bug was symmetric: each handler built a
// fresh VideoStreamUserData and UpdateSessionData deletes the key of
// whichever field is empty.
func TestPutTrackChoiceKeepsTheOtherChoice(t *testing.T) {
	t.Run("subtitle then audio", func(t *testing.T) {
		s := newTrackSession(t)
		s.put("subtitle", "os-os-en")
		s.put("audio", "mp-1")
		sub, aud := s.read()
		if sub != "os-os-en" {
			t.Errorf("an audio write erased the subtitle choice: got %q", sub)
		}
		if aud != "mp-1" {
			t.Errorf("audio = %q, want mp-1", aud)
		}
	})
	t.Run("audio then subtitle", func(t *testing.T) {
		s := newTrackSession(t)
		s.put("audio", "mp-1")
		s.put("subtitle", "os-os-en")
		sub, aud := s.read()
		if aud != "mp-1" {
			t.Errorf("a subtitle write erased the audio choice: got %q", aud)
		}
		if sub != "os-os-en" {
			t.Errorf("subtitle = %q, want os-os-en", sub)
		}
	})
}

// The empty id still has to clear its own key -- that is how the player
// records "subtitles off" is not it, but a deleted upload landing on nothing
// and the retry path both send one. Clearing one choice must not clear the
// other either.
func TestPutTrackChoiceEmptyIDClearsOnlyItsOwnKey(t *testing.T) {
	s := newTrackSession(t)
	s.put("subtitle", "os-os-en")
	s.put("audio", "mp-1")
	s.put("subtitle", "")
	sub, aud := s.read()
	if sub != "" {
		t.Errorf("subtitle = %q, want it cleared", sub)
	}
	if aud != "mp-1" {
		t.Errorf("clearing the subtitle took the audio with it: got %q", aud)
	}
}

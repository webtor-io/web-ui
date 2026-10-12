package action

import (
	"crypto/sha1"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/redis"
	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/tracksession"
)

// redisTrackSession runs the track PUTs on the production session store --
// redis with tracksession installed -- so the 4096-byte refusal is
// redistore's own and not a stand-in. /resource appends to the guest
// resources list the way addResourceToSession does, and /seed writes choices
// in the pre-bound format, without OrderKey.
func redisTrackSession(t *testing.T) (*trackSession, *miniredis.Miniredis) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	mr := miniredis.RunT(t)
	store, err := redis.NewStore(2, "tcp", mr.Addr(), "", []byte("test secret"))
	if err != nil {
		t.Fatal(err)
	}
	if err := tracksession.Install(store); err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.Use(sessions.Sessions("session", store))
	for kind, k := range map[string]trackKind{"subtitle": trackSubtitle, "audio": trackAudio} {
		r.PUT("/stream-video/"+kind, func(c *gin.Context) {
			if err := putTrackChoice(c, k); err != nil {
				c.AbortWithStatus(http.StatusInternalServerError)
			}
		})
	}
	r.PUT("/resource", func(c *gin.Context) {
		s := sessions.Default(c)
		rs, _ := s.Get("resources").([]string)
		s.Set("resources", append(rs, c.Query("id")))
		if err := s.Save(); err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
		}
	})
	r.PUT("/seed", func(c *gin.Context) {
		s := sessions.Default(c)
		n, _ := strconv.Atoi(c.Query("files"))
		for i := 0; i < n; i++ {
			s.Set(file(i)+"_audio_id", "mp-1")
			s.Set(file(i)+"_subtitle_id", "os-os-en")
		}
		if err := s.Save(); err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
		}
	})
	r.GET("/read", func(c *gin.Context) {
		ud := models.NewVideoStreamUserData(c.Query("res"), c.Query("item"), &models.StreamSettings{})
		ud.FetchSessionData(c)
		c.String(http.StatusOK, ud.AudioID+"|"+ud.SubtitleID)
	})
	return &trackSession{t: t, r: r}, mr
}

// file is "<resource>_<item>" in production shape: two 40-hex ids, hashed so
// that their lexical order is not the order of the picks.
func file(i int) string {
	return fmt.Sprintf("%x_%x", sha1.Sum([]byte("res"+strconv.Itoa(i))), sha1.Sum([]byte("item"+strconv.Itoa(i))))
}

func (s *trackSession) pick(i int) {
	s.t.Helper()
	res, item, _ := strings.Cut(file(i), "_")
	body := `{"resourceID":"` + res + `","itemID":"` + item + `","id":"`
	s.do(http.MethodPut, "/stream-video/subtitle", body+`os-os-en"}`)
	s.do(http.MethodPut, "/stream-video/audio", body+`mp-1"}`)
}

func (s *trackSession) choice(i int) string {
	s.t.Helper()
	res, item, _ := strings.Cut(file(i), "_")
	return s.do(http.MethodGet, "/read?res="+res+"&item="+item, "").Body.String()
}

func storedBytes(t *testing.T, mr *miniredis.Miniredis) int {
	t.Helper()
	max := 0
	for _, k := range mr.Keys() {
		if v, err := mr.Get(k); err == nil && strings.HasPrefix(k, "session_") && len(v) > max {
			max = len(v)
		}
	}
	return max
}

// A session that already holds as many choices as fit -- the shape found in
// production, 17 files in 4011 bytes -- must accept the next pick instead of
// failing it with "value too big".
func TestTrackPickOnSaturatedLegacySession(t *testing.T) {
	s, mr := redisTrackSession(t)
	s.do(http.MethodPut, "/seed?files=17", "")
	s.pick(100)
	if got := s.choice(100); got != "mp-1|os-os-en" {
		t.Fatalf("new pick = %q, want both choices", got)
	}
	if n := storedBytes(t, mr); n > tracksession.Budget {
		t.Fatalf("stored session is %d bytes, budget %d", n, tracksession.Budget)
	}
}

// Picks on many files keep the last MaxItems, and the guest resources list,
// which shares the session, keeps growing to its cap of 50 without a failed
// save: the bound is in the serializer, not only in the track handler.
func TestTrackChoicesLeaveRoomForResources(t *testing.T) {
	s, mr := redisTrackSession(t)
	for i := 0; i < 30; i++ {
		s.pick(i)
		if got := s.choice(i); got != "mp-1|os-os-en" {
			t.Fatalf("file %d: just picked, read back %q", i, got)
		}
	}
	if got := s.choice(30 - tracksession.MaxItems); got != "mp-1|os-os-en" {
		t.Fatalf("oldest of the last %d files lost its choice: %q", tracksession.MaxItems, got)
	}
	if got := s.choice(30 - tracksession.MaxItems - 1); got != "|" {
		t.Fatalf("file beyond the last %d kept its choice: %q", tracksession.MaxItems, got)
	}
	for i := 0; i < 50; i++ {
		s.do(http.MethodPut, fmt.Sprintf("/resource?id=%040x", 5000+i), "")
	}
	if got := s.choice(29); got != "mp-1|os-os-en" {
		t.Fatalf("latest pick lost after resources grew: %q", got)
	}
	if n := storedBytes(t, mr); n > tracksession.Budget {
		t.Fatalf("stored session is %d bytes, budget %d", n, tracksession.Budget)
	}
}

package j

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	ra "github.com/webtor-io/rest-api/services"

	"github.com/webtor-io/web-ui/jobs/scripts"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/job"
	"github.com/webtor-io/web-ui/services/web"
)

// An upload and a magnet with tr= run per user: what rest-api answers
// depends on who asks. A bare magnet runs the shared run, as before.
func TestLoadJobID(t *testing.T) {
	const h = "0123456789abcdef0123456789abcdef01234567"
	uidA, uidB := strings.Repeat("a", 40), strings.Repeat("b", 40)
	sha := func(s string) string { return fmt.Sprintf("%x", sha1.Sum([]byte(s))) }
	file := &scripts.LoadArgs{File: []byte("x")}
	withTr := &scripts.LoadArgs{Query: "magnet:?xt=urn:btih:" + h + "&tr=http%3A%2F%2Ftracker.example%2Fannounce"}
	bare := &scripts.LoadArgs{Query: "magnet:?xt=urn:btih:" + h}
	a := &web.Context{Lang: "en", ApiClaims: &api.Claims{Uid: uidA}}
	b := &web.Context{Lang: "en", ApiClaims: &api.Claims{Uid: uidB}}
	anon := &web.Context{Lang: "en", ApiClaims: &api.Claims{}}
	none := &web.Context{Lang: "en"} // the api middleware did not run
	for _, c := range []struct {
		name      string
		got, want string
	}{
		{"upload, user A", loadJobID(a, file, h), sha(h + "/en/" + uidA)},
		{"upload, user B", loadJobID(b, file, h), sha(h + "/en/" + uidB)},
		{"magnet with tr=, user A", loadJobID(a, withTr, h), sha(h + "/en/" + uidA)},
		{"magnet with tr=, anonymous", loadJobID(anon, withTr, h), sha(h + "/en/")},
		{"upload, no claims", loadJobID(none, file, h), loadJobID(anon, file, h)},
		{"bare magnet, user A", loadJobID(a, bare, h), sha(h + "/en")},
	} {
		if c.got != c.want {
			t.Errorf("%s: id %s, want %s", c.name, c.got, c.want)
		}
	}
	// An anonymous card is not the shared run a signed-in user's bare magnet
	// replays.
	if loadJobID(anon, withTr, h) == sha(h+"/en") {
		t.Error("an anonymous user's magnet with tr= runs as the shared run")
	}
}

// A user's own run is not kept for replay: the same user's next magnet (a new
// passkey) runs again. The shared run of a bare magnet is kept as before.
func TestPerUserRunIsNotKept(t *testing.T) {
	torrent, h := privateTorrent(t)
	uidA := strings.Repeat("a", 40)
	posted := make(chan jwt.MapClaims, 8)
	restAPI := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/resource/"+h:
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/resource":
			p, err := jwt.Parse(r.Header.Get("X-Token"), func(*jwt.Token) (interface{}, error) { return []byte("test"), nil })
			if err != nil || !p.Valid {
				t.Errorf("X-Token did not validate: %v", err)
				posted <- jwt.MapClaims{}
			} else {
				posted <- p.Claims.(jwt.MapClaims)
			}
			_ = json.NewEncoder(w).Encode(ra.ResourceResponse{ID: h})
		default:
			t.Errorf("unexpected %v %v", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
		}
	})
	st := &memStorage{logs: map[string][]job.LogItem{}, closed: make(chan string, 8)}
	jobs := testJobsWith(t, restAPI, st)()
	a := &web.Context{Lang: "en", ApiClaims: &api.Claims{Uid: uidA}}
	anon := &web.Context{Lang: "en", ApiClaims: &api.Claims{}}
	for _, c := range []struct {
		name      string
		c         *web.Context
		args      *scripts.LoadArgs
		cacheable bool
		uid       any // the POST's uid claim; nil is none
	}{
		{"magnet with tr=, user A", a, &scripts.LoadArgs{Query: "magnet:?xt=urn:btih:" + h + "&tr=http%3A%2F%2Ftracker.example%2Fannounce"}, false, uidA},
		{"upload, user A", a, &scripts.LoadArgs{File: torrent, Source: "site"}, false, uidA},
		{"upload, anonymous", anon, &scripts.LoadArgs{File: torrent, Source: "site"}, false, nil},
		{"bare magnet, user A", a, &scripts.LoadArgs{Query: "magnet:?xt=urn:btih:" + h}, true, uidA},
	} {
		t.Run(c.name, func(t *testing.T) {
			run, err := jobs.Load(c.c, c.args)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := st.waitClosed(t, c.name), loadJobID(c.c, c.args, h); got != want {
				t.Fatalf("closed %s, want %s", got, want)
			}
			if run.Cacheable() != c.cacheable {
				t.Errorf("Cacheable() = %v, want %v", run.Cacheable(), c.cacheable)
			}
			select {
			case cl := <-posted:
				if cl["uid"] != c.uid {
					t.Errorf("POST claims %v, want uid %v", cl, c.uid)
				}
			default:
				t.Error("the run closed without a POST /resource")
			}
		})
	}
}

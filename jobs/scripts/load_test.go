package scripts

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/golang-jwt/jwt/v5"
	log "github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/urfave/cli"

	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/job"
	"github.com/webtor-io/web-ui/services/web"
)

// A hash torrent-store refuses (rest-api's 403) is not a dead magnet: on the
// lookup by hash or on the store of the magnet it ends as error.forbidden,
// never as the "nobody shares it, try ten minutes" card. A magnet that did
// not resolve and a broken link are still the card.
func TestStoreQueryRefusal(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	cases := []struct {
		name      string
		get, post int    // rest-api's answers; a zero post is never asked
		body      string // the POST's error body
		key       string // ClassifyError of the plain error, or of the card's (an embed has no card)
		kind      string // the card's MagnetError.Kind
	}{
		{name: "lookup refused", get: http.StatusForbidden, key: "error.forbidden"},
		{name: "store refused", get: http.StatusNotFound, post: http.StatusForbidden, body: `{"error":"forbidden"}`, key: "error.forbidden"},
		{name: "nobody shares it", get: http.StatusNotFound, post: http.StatusInternalServerError, body: `{"error":"magnet timeout"}`, kind: "dead"},
		{name: "broken link", get: http.StatusNotFound, post: http.StatusBadRequest, body: `{"error":"failed to parse magnet: placeholder"}`, kind: "invalid"},
		// rest-api resolved the magnet to a private torrent and stored
		// nothing: its own card (upload the .torrent), never the dead one
		// with its ten-minute retry -- no peer will change the answer.
		{name: "private torrent", get: http.StatusNotFound, post: http.StatusUnprocessableEntity, body: `{"error":"private torrent: this magnet has no tracker; upload the .torrent file or use a magnet that includes your tracker"}`, kind: "private", key: "error.private_magnet"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/resource/"+hash:
					w.WriteHeader(tc.get)
				case r.Method == http.MethodPost && r.URL.Path == "/resource" && tc.post != 0:
					w.WriteHeader(tc.post)
					_, _ = w.Write([]byte(tc.body))
				default:
					t.Errorf("unexpected %v %v", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusTeapot)
				}
			}))
			defer srv.Close()
			s := testLoadScript(t, srv, &api.Claims{})
			j := job.New(context.Background(), "t", "test", nil, &job.NilStorage{}, false, nil)

			_, err := s.storeQuery(context.Background(), j, "magnet:?xt=urn:btih:"+hash)
			// The assertion jobs/load.go makes to pick the card.
			me, card := err.(*MagnetError)
			if tc.kind != "" {
				if !card || me.Kind != tc.kind {
					t.Fatalf("err=%v, want the %s card", err, tc.kind)
				}
				if tc.key != "" {
					if got := web.ClassifyError(err); got != tc.key {
						t.Fatalf("ClassifyError(%q)=%s, want %s", err, got, tc.key)
					}
				}
				return
			}
			if err == nil || card {
				t.Fatalf("err=%v, want a plain error, not the card", err)
			}
			if got := web.ClassifyError(err); got != tc.key {
				t.Fatalf("ClassifyError(%q)=%s, want %s", err, got, tc.key)
			}
		})
	}
}

// testLoadScript is a LoadScript for a user with claims c, talking to srv as
// its rest-api; tokens are signed with the secret "test".
func testLoadScript(t *testing.T, srv *httptest.Server, c *api.Claims) *LoadScript {
	t.Helper()
	u, _ := url.Parse(srv.URL)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range api.RegisterFlags(nil) {
		f.Apply(fs)
	}
	if err := fs.Parse([]string{"--webtor-rest-api-host=" + u.Hostname(), "--webtor-rest-api-port=" + u.Port(),
		"--webtor-secret=test", "--rapidapi-host="}); err != nil {
		t.Fatal(err)
	}
	return &LoadScript{
		api:  api.New(cli.NewContext(cli.NewApp(), fs, nil), srv.Client()),
		i18n: i18n.New(os.DirFS("../../locales")),
		c:    &web.Context{Lang: "en", ApiClaims: c},
		args: &LoadArgs{},
	}
}

// The lookup that decides "new or known" is the shared run's, so it reads as
// internal; only the store of a user's own magnet carries the user. A known
// private torrent still gets the magnet with tr= stored: that is how the
// user's own tracker reaches rest-api. A rest-api without the private field
// (before phase 1) reads as public: no POST, as before.
func TestStoreQueryLooksUpAsInternal(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	const uid = "89abcdef0123456789abcdef0123456789abcdef"
	withTr := "magnet:?xt=urn:btih:" + hash + "&tr=http%3A%2F%2Ftracker.example%2Fannounce"
	bare := "magnet:?xt=urn:btih:" + hash
	cases := []struct {
		name, query string
		get         string // the GET's body; empty is a 404
		post        bool
		private     bool
	}{
		{name: "unknown, tr=", query: withTr, post: true},
		{name: "known public, tr=", query: withTr, get: `{"id":"` + hash + `","private":false}`},
		{name: "known private, tr=", query: withTr, get: `{"id":"` + hash + `","private":true}`, post: true, private: true},
		{name: "known private, bare", query: bare, get: `{"id":"` + hash + `","private":true}`, private: true},
		{name: "rest-api before phase 1", query: withTr, get: `{"id":"` + hash + `"}`},
	}
	claimsOf := func(t *testing.T, r *http.Request) jwt.MapClaims {
		p, err := jwt.Parse(r.Header.Get("X-Token"), func(*jwt.Token) (interface{}, error) { return []byte("test"), nil })
		if err != nil || !p.Valid {
			t.Errorf("X-Token did not validate: %v", err)
			return jwt.MapClaims{}
		}
		return p.Claims.(jwt.MapClaims)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			posted := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/resource/"+hash:
					if cl := claimsOf(t, r); cl["internal"] != true || cl["uid"] != nil {
						t.Errorf("GET claims %v, want internal and no uid", cl)
					}
					if tc.get == "" {
						w.WriteHeader(http.StatusNotFound)
						return
					}
					_, _ = w.Write([]byte(tc.get))
				case r.Method == http.MethodPost && r.URL.Path == "/resource":
					posted = true
					if cl := claimsOf(t, r); cl["uid"] != uid || cl["internal"] != nil {
						t.Errorf("POST claims %v, want uid and no internal", cl)
					}
					_, _ = w.Write([]byte(`{"id":"` + hash + `"}`))
				default:
					t.Errorf("unexpected %v %v", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusTeapot)
				}
			}))
			defer srv.Close()
			s := testLoadScript(t, srv, &api.Claims{Uid: uid})
			j := job.New(context.Background(), "t", "test", nil, &job.NilStorage{}, false, nil)

			res, err := s.storeQuery(context.Background(), j, tc.query)
			if err != nil {
				t.Fatal(err)
			}
			if posted != tc.post {
				t.Errorf("POST /resource: %v, want %v", posted, tc.post)
			}
			if res == nil || res.ID != hash || res.Private != tc.private {
				t.Errorf("res %+v, want id %s private %v", res, hash, tc.private)
			}
		})
	}
}

// The embed hands Load a magnet from the embedding page's settings; one Load
// refuses becomes the job's error, which is logged ("got job error"). It
// says how long the query was and what it was, without the trackers and
// sources in it: a private tracker's key rides in their URLs.
func TestLoad_RefusedQueryKeepsNoTracker(t *testing.T) {
	const passkey = "0123456789abcdef0123456789abcdef"
	q := "magnet:?xt=urn:btih:5e4bd524&tr=http%3A%2F%2Ftracker.invalid%2Fannounce%3Fpasskey%3D" + passkey + "&as=https%3A%2F%2Ftracker.invalid%2F1." + passkey
	_, _, err := Load(nil, nil, &web.Context{Lang: "en"}, &LoadArgs{Query: q})
	if err == nil {
		t.Fatal("the broken magnet was accepted")
	}
	if msg := err.Error(); strings.Contains(msg, passkey) || strings.Contains(msg, "tracker.invalid") {
		t.Errorf("the error quotes the trackers: %s", msg)
	}
	if msg := err.Error(); !strings.Contains(msg, "query_len="+strconv.Itoa(len(q))+` query="magnet:?xt=urn:btih:5e4bd524&tr=<redacted>&as=<redacted>"`) {
		t.Errorf("want query_len=%d and the redacted query in %s", len(q), msg)
	}
	if got := web.ClassifyError(err); got != "error.magnet_invalid" {
		t.Errorf("ClassifyError = %s, want error.magnet_invalid", got)
	}
	_, _, err = Load(nil, nil, &web.Context{Lang: "en"}, &LoadArgs{Query: "Big Buck Bunny 2008"})
	if err == nil || !strings.Contains(err.Error(), `query="Big Buck Bunny 2008"`) {
		t.Errorf("a refused title must be readable: %v", err)
	}
}

// A private .torrent submitted on the site (or fetched for an embed) is
// counted once, by the door it came through (services/private_torrent).
func TestLoad_CountsAPrivateUpload(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()
	info := metainfo.Info{Name: "probe", PieceLength: 16384, Length: 1, Pieces: make([]byte, 20), Private: &[]bool{true}[0]}
	mi := metainfo.MetaInfo{InfoBytes: bencode.MustMarshal(info), Announce: "http://tracker.invalid/announce?passkey=0123456789abcdef0123456789abcdef"}
	var b bytes.Buffer
	if err := mi.Write(&b); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(nil, nil, &web.Context{Lang: "en"}, &LoadArgs{File: b.Bytes(), Source: "embed"}); err != nil {
		t.Fatal(err)
	}
	var lines []*log.Entry
	for _, e := range hook.AllEntries() {
		if e.Data["event"] == "private_torrent_upload" {
			lines = append(lines, e)
		}
	}
	if len(lines) != 1 || lines[0].Data["source"] != "embed" || lines[0].Data["trackers"] != "http://tracker.invalid" {
		t.Fatalf("lines %v, want one from the embed", hook.AllEntries())
	}
}

// The dev playback of the private card (debug=magnet_private) is the real
// card's error: Kind private with the refusal inside, so the card, its
// err_key and an embed's plain message are the ones a real refusal gets.
func TestDebugMagnetPrivate(t *testing.T) {
	s := &LoadScript{
		i18n: i18n.New(os.DirFS("../../locales")),
		c:    &web.Context{Lang: "en", ApiClaims: &api.Claims{}},
		args: &LoadArgs{},
	}
	j := job.New(context.Background(), "t", "test", nil, &job.NilStorage{}, false, nil)
	err := s.debugMagnet(context.Background(), j, "magnet_private")
	me, card := err.(*MagnetError)
	if !card || me.Kind != "private" || !errors.Is(err, api.ErrPrivateMagnet) {
		t.Fatalf("err=%v, want the private card with api.ErrPrivateMagnet inside", err)
	}
}

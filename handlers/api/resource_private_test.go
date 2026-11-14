package api

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/urfave/cli"

	restapi "github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/libapi"
)

// newRestAPIHandler is a Handler whose rest-api is upstream.
func newRestAPIHandler(t *testing.T, upstream *httptest.Server) *Handler {
	t.Helper()
	u, _ := url.Parse(upstream.URL)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range restapi.RegisterFlags(nil) {
		f.Apply(fs)
	}
	if err := fs.Parse([]string{"--webtor-rest-api-host=" + u.Hostname(), "--webtor-rest-api-port=" + u.Port(),
		"--webtor-secret=test", "--rapidapi-host="}); err != nil {
		t.Fatal(err)
	}
	return &Handler{api: restapi.New(cli.NewContext(cli.NewApp(), fs, nil), upstream.Client())}
}

// A private .torrent stored through the JSON API is counted once, as the
// API's (services/private_torrent); a magnet is not a torrent and is not.
func TestPostResource_CountsAPrivateUpload(t *testing.T) {
	info := metainfo.Info{Name: "probe", PieceLength: 16384, Length: 1, Pieces: make([]byte, 20), Private: &[]bool{true}[0]}
	mi := metainfo.MetaInfo{InfoBytes: bencode.MustMarshal(info), Announce: "http://tracker.invalid/announce?passkey=0123456789abcdef0123456789abcdef"}
	var torrent bytes.Buffer
	if err := mi.Write(&torrent); err != nil {
		t.Fatal(err)
	}
	hash := mi.HashInfoBytes().HexString()

	restAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/resource" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"id":"` + hash + `"}`))
	}))
	t.Cleanup(restAPI.Close)
	h := newRestAPIHandler(t, restAPI)
	gin.SetMode(gin.TestMode)

	count := func(body []byte) int {
		hook := test.NewGlobal()
		defer hook.Reset()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/resource", bytes.NewReader(body))
		h.postResource(c)
		if w.Code != http.StatusOK {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		n := 0
		for _, e := range hook.AllEntries() {
			if e.Data["event"] == "private_torrent_upload" {
				if e.Data["source"] != "api" || e.Data["user"] != "anon" || e.Data["infohash"] != hash {
					t.Errorf("line %v", e.Data)
				}
				n++
			}
		}
		return n
	}
	if n := count(torrent.Bytes()); n != 1 {
		t.Errorf("a private torrent: %d lines, want 1", n)
	}
	if n := count([]byte("magnet:?xt=urn:btih:" + hash)); n != 0 {
		t.Errorf("a magnet: %d lines, want 0", n)
	}
}

// rest-api refuses a magnet of a private torrent it has not stored with 422
// and stores nothing. The API passes that on as 422 with a code of its own, so
// a client can tell "upload the .torrent instead" from a failure: not the 502
// "webtor is broken" that an unrecognised upstream error becomes, and not the
// 404 "could not fetch" of a magnet nobody shares.
func TestPostResourcePrivateMagnet(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/resource" {
			t.Errorf("unexpected %v %v", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error": "private torrent: this magnet has no tracker; upload the .torrent file or use a magnet that includes your tracker"}`))
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	h := newRestAPIHandler(t, upstream)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), restapi.ClaimsContext{}, &restapi.Claims{}))
		c.Next()
	})
	r.POST("/resource", h.postResource)
	req := httptest.NewRequest(http.MethodPost, "/resource", strings.NewReader("magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422: %s", w.Code, w.Body.String())
	}
	var res libapi.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("body is not an error document: %q", w.Body.String())
	}
	if res.Error.Code != libapi.CodePrivateMagnet {
		t.Errorf("code %q, want %q", res.Error.Code, libapi.CodePrivateMagnet)
	}
	for _, w := range []string{"no tracker", ".torrent", "magnet that includes the tracker"} {
		if !strings.Contains(res.Error.Message, w) {
			t.Errorf("the message must say why and what to do instead (%q): %q", w, res.Error.Message)
		}
	}
	// The upstream text (it carries rest-api's internal URL) stays in the logs.
	if strings.Contains(w.Body.String(), u.Host) {
		t.Errorf("upstream URL leaked into the response: %s", w.Body.String())
	}
}

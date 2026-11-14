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
	return restAPIHandler(t, upstream.Client(), "--webtor-rest-api-host="+u.Hostname(), "--webtor-rest-api-port="+u.Port(),
		"--webtor-secret=test", "--rapidapi-host=")
}

// restAPIHandler is a Handler whose rest-api client New builds from flags,
// talking through cl.
func restAPIHandler(t *testing.T, cl *http.Client, flags ...string) *Handler {
	t.Helper()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range restapi.RegisterFlags(nil) {
		f.Apply(fs)
	}
	if err := fs.Parse(flags); err != nil {
		t.Fatal(err)
	}
	return &Handler{api: restapi.New(cli.NewContext(cli.NewApp(), fs, nil), cl)}
}

// privateTorrent is a private .torrent with a test key in its announce, and
// its hash.
func privateTorrent(t *testing.T) ([]byte, string) {
	t.Helper()
	info := metainfo.Info{Name: "probe", PieceLength: 16384, Length: 1, Pieces: make([]byte, 20), Private: &[]bool{true}[0]}
	mi := metainfo.MetaInfo{InfoBytes: bencode.MustMarshal(info), Announce: "http://tracker.invalid/announce?passkey=0123456789abcdef0123456789abcdef"}
	var torrent bytes.Buffer
	if err := mi.Write(&torrent); err != nil {
		t.Fatal(err)
	}
	return torrent.Bytes(), mi.HashInfoBytes().HexString()
}

// A private .torrent stored through the JSON API is counted once, as the
// API's (services/private_torrent); a magnet is not a torrent and is not.
func TestPostResource_CountsAPrivateUpload(t *testing.T) {
	torrent, hash := privateTorrent(t)

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
	if n := count(torrent); n != 1 {
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

// rest-api's coded refusals reach a JSON API client as rest-api named them:
// its status, code, reason and Retry-After, so a client can tell "sign in" from
// "your tracker refused the key" from "try again in 5 s". A 403 without a code
// stays the plan's forbidden.
func TestUpstreamPrivateEnvelope(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	for _, c := range []struct {
		path, method         string
		status               int
		body, retry          string
		wantStatus           int
		wantCode, wantReason string
	}{
		{"/resource/" + hash, "GET", 403, `{"error":"private torrent: only a signed-in user who added it with their own tracker can play it","code":"private_torrent","reason":"no_record"}`, "", 403, "private_torrent", "no_record"},
		{"/resource", "POST", 422, `{"error":"x","code":"private_tracker_refused","reason":"account"}`, "", 422, "private_tracker_refused", "account"},
		{"/resource", "POST", 503, `{"error":"x","code":"private_verify_unavailable"}`, "5", 503, "private_verify_unavailable", ""},
		{"/resource", "POST", 429, `{"error":"x"}`, "60", 429, "rate_limited", ""},
		{"/resource/" + hash, "GET", 403, `{"error":"restricted"}`, "", 403, "forbidden", ""},
	} {
		// representatives only (S32): the full code table is TestRestAPIPrivateCodes (Task 1)
		t.Run(c.wantCode, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if c.retry != "" {
					w.Header().Set("Retry-After", c.retry)
				}
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
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
			r.GET("/resource/:resource_id", h.getResource)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(c.method, c.path, strings.NewReader("magnet:?xt=urn:btih:"+hash)))

			if w.Code != c.wantStatus {
				t.Errorf("status %d, want %d: %s", w.Code, c.wantStatus, w.Body.String())
			}
			// The wire shape, not the Go type: what a client decodes.
			var res struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
					Reason  string `json:"reason"`
				} `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
				t.Fatalf("body is not an error document: %q", w.Body.String())
			}
			if res.Error.Code != c.wantCode || res.Error.Reason != c.wantReason {
				t.Errorf("code %q reason %q, want %q %q", res.Error.Code, res.Error.Reason, c.wantCode, c.wantReason)
			}
			if got := w.Header().Get("Retry-After"); got != c.retry {
				t.Errorf("Retry-After %q, want %q", got, c.retry)
			}
			if strings.Contains(w.Body.String(), u.Host) {
				t.Errorf("upstream URL leaked into the response: %s", w.Body.String())
			}
		})
	}
}

// GET /resource/{id} answers rest-api's private flag as it came. The module's
// own field is shadowed by services/api.Resource, and decoding left it false
// for every private torrent until GetResource copied the flag down.
func TestGetResourceKeepsPrivate(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	for _, want := range []bool{true, false} {
		body := `{"id":"` + hash + `","name":"probe","private":false}`
		if want {
			body = `{"id":"` + hash + `","name":"probe","private":true}`
		}
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		h := newRestAPIHandler(t, upstream)
		gin.SetMode(gin.TestMode)
		r := gin.New()
		r.Use(func(c *gin.Context) {
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), restapi.ClaimsContext{}, &restapi.Claims{}))
			c.Next()
		})
		r.GET("/resource/:resource_id", h.getResource)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/resource/"+hash, nil))
		upstream.Close()
		var res struct {
			Private *bool `json:"private"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil || w.Code != http.StatusOK {
			t.Fatalf("status %d body %q: %v", w.Code, w.Body.String(), err)
		}
		if res.Private == nil || *res.Private != want {
			t.Errorf("rest-api said private=%t, the API answered %s", want, w.Body.String())
		}
	}
}

package resource

import (
	"encoding/json"
	"flag"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gin-contrib/multitemplate"
	"github.com/gin-gonic/gin"
	"github.com/urfave/cli"

	"github.com/webtor-io/web-ui/handlers/index"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/web"
)

const (
	hashMissing = "0000000000000000000000000000000000000001"
	hashBanned  = "0000000000000000000000000000000000000002"
	hashBroken  = "0000000000000000000000000000000000000003"
)

// fakeRestAPI answers /resource/<hash> the way rest-api does for the three
// outcomes the resource page cannot render: no torrent (404), a refused one
// (banned or stoplisted: 403) and a failure on the way (500).
func fakeRestAPI(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch strings.TrimPrefix(r.URL.Path, "/resource/") {
		case hashMissing:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found sha1=` + hashMissing + `"}`))
		case hashBanned:
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"forbidden: restricted by the rightholder"}`))
		case hashBroken:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"failed to pull torrent: context deadline exceeded"}`))
		default:
			t.Errorf("unexpected rest-api call %s", r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

// newResourceRouter wires the resource and index handlers the way serve.go
// does, against the fake rest-api and a stand-in index view that prints what
// the real one branches on. The template manager resolves "templates/"
// against the working directory, hence the chdir.
func newResourceRouter(t *testing.T, restAPI *httptest.Server) *gin.Engine {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"templates/layouts/main.html": `{{ template "main" . }}`,
		"templates/views/index.html":  `{{ define "main" }}[home][err={{ .ErrKey }}][instruction={{ .Data.Instruction }}]{{ end }}`,
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{"templates/partials", "templates/views/resource"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)

	u, err := url.Parse(restAPI.URL)
	if err != nil {
		t.Fatal(err)
	}
	host, port, _ := net.SplitHostPort(u.Host)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range append(api.RegisterFlags(nil), common.RegisterFlags(nil)...) {
		f.Apply(fs)
	}
	// Explicit values for everything the environment could otherwise fill
	// in: the flags read their env vars on Apply, and a developer shell with
	// RAPIDAPI_HOST set would send these requests to the real API.
	if err := fs.Parse([]string{
		"--webtor-rest-api-host", host,
		"--webtor-rest-api-port", port,
		"--webtor-rest-api-secure=false",
		"--rapidapi-host", "",
		"--rapidapi-key", "",
		"--" + common.SessionSecretFlag, "test-secret",
	}); err != nil {
		t.Fatal(err)
	}
	c := cli.NewContext(cli.NewApp(), fs, nil)
	if !c.BoolT(common.UseDirectLinks) {
		t.Fatal("direct links are expected on by default; the session check would need a session store")
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(web.NoindexDefault(false))
	re := multitemplate.NewRenderer()
	r.HTMLRender = re
	tm := template.NewManager[*web.Context](re)
	RegisterHandler(c, r, tm, api.New(c, restAPI.Client()), nil, nil, nil, nil)
	index.RegisterHandler(r, tm, nil)
	if err := tm.Init(); err != nil {
		t.Fatal(err)
	}
	return r
}

// A resource URL that names nothing answers 404 with the home page itself --
// the reason above the form -- and stays noindex. It used to be a 302 to /?err=,
// i.e. a 200 home page, which is why dead hash links could never leave the
// search indexes.
func TestResourceGet_NamesNothing_Is404HomePage(t *testing.T) {
	restAPI, seen := fakeRestAPI(t)
	r := newResourceRouter(t, restAPI)

	cases := []struct {
		path string
		key  string
	}{
		// Not an ID at all: no run of five hex digits, rest-api is never asked.
		{"/wp-login.php", "error.invalid_resource"},
		{"/library", "error.invalid_resource"},
		{"/this-page-does-not-exist", "error.invalid_resource"},
		{"/index.html", "error.invalid_resource"},
		// An ID nobody has: a share link whose torrent is gone.
		{"/" + hashMissing, "error.not_found"},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404 (Location %q)", tc.path, w.Code, w.Header().Get("Location"))
		}
		if got := w.Header().Get("X-Robots-Tag"); !strings.HasPrefix(got, "noindex") {
			t.Errorf("%s: X-Robots-Tag %q, want noindex", tc.path, got)
		}
		body := w.Body.String()
		if !strings.Contains(body, "[home][err="+tc.key+"]") {
			t.Errorf("%s: body %q, want the home page with %s", tc.path, body, tc.key)
		}
		// The home form carries the instruction in a hidden field; the
		// path of a dead URL must not become one.
		if !strings.Contains(body, "[instruction=]") {
			t.Errorf("%s: body %q, want an empty instruction", tc.path, body)
		}
	}
	for _, p := range seen() {
		if p != "/resource/"+hashMissing {
			t.Errorf("rest-api asked for %s: an invalid ID must not reach it", p)
		}
	}
}

// The async navigation (data-async links, X-Requested-With) renders whatever
// body comes back, whatever the status: the dead link still shows the page.
func TestResourceGet_NamesNothing_AsyncGetsTheFragment(t *testing.T) {
	restAPI, _ := fakeRestAPI(t)
	r := newResourceRouter(t, restAPI)

	req := httptest.NewRequest(http.MethodGet, "/"+hashMissing, nil)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("X-Layout", `{{ template "main" . }}`)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", w.Code)
	}
	if !strings.Contains(w.Body.String(), `<template data-async-fragment="main">[home][err=error.not_found]`) {
		t.Errorf("body %q, want the main fragment of the home page", w.Body.String())
	}
}

func TestResourceGet_NamesNothing_JSON(t *testing.T) {
	restAPI, _ := fakeRestAPI(t)
	r := newResourceRouter(t, restAPI)

	req := httptest.NewRequest(http.MethodGet, "/"+hashMissing, nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", w.Code)
	}
	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("body %q: %v", w.Body.String(), err)
	}
	if got["status"] != "error" || got["message"] != "error.not_found" {
		t.Errorf("body %v, want status=error message=error.not_found", got)
	}
}

// What stays a redirect: a refused resource (its policy is separate), a
// failure on our side (says nothing about the URL), and every form error --
// the POST and the magnet GET that shares its handler.
func TestResourceGet_OtherFailuresStillRedirect(t *testing.T) {
	restAPI, _ := fakeRestAPI(t)
	r := newResourceRouter(t, restAPI)

	cases := []struct {
		method, path, form string
		key                string
	}{
		{http.MethodGet, "/" + hashBanned, "", "error.forbidden"},
		{http.MethodGet, "/" + hashBroken, "", "error.generic"},
		{http.MethodPost, "/", "resource=not+a+magnet", "error.free_text"},
		{http.MethodGet, "/magnet:?xt=urn:btih:zz", "", "error.magnet_invalid"},
	}
	for _, tc := range cases {
		var req *http.Request
		if tc.form != "" {
			req = httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.form))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			req = httptest.NewRequest(tc.method, tc.path, nil)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusFound {
			t.Errorf("%s %s: status %d, want 302", tc.method, tc.path, w.Code)
			continue
		}
		loc, err := url.Parse(w.Header().Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		if loc.Path != "/" || loc.Query().Get("err") != tc.key || loc.Query().Get("status") != "error" {
			t.Errorf("%s %s: Location %q, want /?err=%s&status=error", tc.method, tc.path, loc, tc.key)
		}
	}
}

// Keep the fixture hashes honest: the handler only calls rest-api for an ID
// that passes the hex check, so a fixture that does not would silently test
// the invalid branch instead.
func TestResourceGet_FixtureHashesAreHashes(t *testing.T) {
	for _, h := range []string{hashMissing, hashBanned, hashBroken} {
		if len(h) != 40 || common.SHA1R.FindString(h) != h {
			t.Errorf("%s is not a 40-hex infohash", h)
		}
	}
}

// A pasted infohash one character short is a form error like the others -- a
// redirect back to the form -- and its numbers go with it, for the sentence
// "it has 39 characters, a full one has 40".
func TestPostHashOfTheWrongLengthSaysSo(t *testing.T) {
	restAPI, seen := fakeRestAPI(t)
	r := newResourceRouter(t, restAPI)

	const sintel = "08ada5a7a6183aae1e09d831df6748d566095a10"
	for _, tc := range []struct{ query, count, full string }{
		{sintel[:39], "39", "40"},
		{sintel + "f", "41", "40"},
	} {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("resource="+tc.query))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Return-Url", "/magnet-to-torrent")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusFound {
			t.Fatalf("%s: status %d, want 302", tc.query, w.Code)
		}
		loc, err := url.Parse(w.Header().Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		q := loc.Query()
		if loc.Path != "/magnet-to-torrent" || q.Get("err") != "error.hash_length" || q.Get("err_count") != tc.count || q.Get("err_full") != tc.full {
			t.Errorf("%s: Location %q, want /magnet-to-torrent?err=error.hash_length&err_count=%s&err_full=%s", tc.query, loc, tc.count, tc.full)
		}
	}
	if s := seen(); len(s) != 0 {
		t.Errorf("rest-api asked for %v: a refused input must not reach it", s)
	}
}

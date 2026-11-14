package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	restapi "github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/libapi"
)

// gateway answers with h whatever web-ui sends through RapidAPI, whose
// address is fixed at https://<host>:443, and counts the requests.
type gateway struct {
	h http.Handler
	n int
}

func (g *gateway) RoundTrip(r *http.Request) (*http.Response, error) {
	g.n++
	w := httptest.NewRecorder()
	g.h.ServeHTTP(w, r)
	return w.Result(), nil
}

// rapidAPIRouter serves the JSON API's resource routes with rest-api reached
// through RapidAPI and answered by g.
func rapidAPIRouter(t *testing.T, g *gateway) *gin.Engine {
	t.Helper()
	h := restAPIHandler(t, &http.Client{Transport: g}, "--rapidapi-host=gateway.invalid", "--rapidapi-key=0000")
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/v1/resource", h.postResource)
	r.GET("/api/v1/resource/:resource_id", h.getResource)
	r.GET("/api/v1/resource/:resource_id/list", h.listResource)
	r.GET("/api/v1/resource/:resource_id/export/:content_id", h.exportResource)
	return r
}

// wantEnvelope says the answer is status with code in the envelope.
func wantEnvelope(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status %d, want %d: %s", w.Code, status, w.Body.String())
	}
	var res libapi.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil || res.Error.Code != code {
		t.Errorf("body %s, want code %s", w.Body.String(), code)
	}
}

// Through RapidAPI one uid serves every user of the instance (decision 42):
// the JSON API refuses a private .torrent with 422 private_unavailable in its
// envelope, and rest-api is not asked.
func TestJSONAPIRapidAPIModePrivate(t *testing.T) {
	torrent, _ := privateTorrent(t)
	g := &gateway{h: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})}
	w := httptest.NewRecorder()
	rapidAPIRouter(t, g).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/resource", bytes.NewReader(torrent)))
	wantEnvelope(t, w, http.StatusUnprocessableEntity, restapi.CodePrivateUnavailable)
	if g.n != 0 {
		t.Errorf("rest-api got %d requests, want none", g.n)
	}
}

// A private resource the instance's account can read (a magnet with tr= made
// it its record) is read by nobody: the resource, its list and its export are
// 403 private_unavailable. So is rest-api's private_torrent, which there
// names that account, not the user. A public resource reads as before.
func TestJSONAPIRapidAPIModePrivateReads(t *testing.T) {
	const (
		private  = "0123456789abcdef0123456789abcdef01234567"
		noRecord = "1123456789abcdef0123456789abcdef01234567"
		public   = "2123456789abcdef0123456789abcdef01234567"
	)
	g := &gateway{h: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/resource/" + private:
			_, _ = w.Write([]byte(`{"id":"` + private + `","name":"probe","private":true}`))
		case "/resource/" + public:
			_, _ = w.Write([]byte(`{"id":"` + public + `","name":"probe","private":false}`))
		case "/resource/" + noRecord:
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"x","code":"private_torrent","reason":"no_record"}`))
		case "/resource/" + private + "/list", "/resource/" + public + "/list":
			_, _ = w.Write([]byte(`{"items":[{"id":"f","name":"probe"}],"items_count":1}`))
		case "/resource/" + private + "/export/f", "/resource/" + public + "/export/f":
			_, _ = w.Write([]byte(`{"exports":{"download":{"url":"https://download.invalid/f"}}}`))
		default:
			t.Errorf("unexpected %v %v", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
		}
	})}
	r := rapidAPIRouter(t, g)
	for _, path := range []string{"", "/list", "/export/f"} {
		for _, hash := range []string{private, noRecord} {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/resource/"+hash+path, nil))
			wantEnvelope(t, w, http.StatusForbidden, restapi.CodePrivateUnavailable)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/resource/"+public+path, nil))
		if w.Code != http.StatusOK {
			t.Errorf("public %q: status %d: %s", path, w.Code, w.Body.String())
		}
	}
}

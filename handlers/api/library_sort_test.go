package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	cs "github.com/webtor-io/common-services"
)

// sort=year is only meaningful with the movies/series sections — the year
// lives in their metadata tables and a bare torrent has none. This pins the
// validation matrix: the invalid combinations die with 400 before touching
// the database, the valid one passes validation (and then fails on the
// absent test DB with 503, which is the proof it got through).
func TestListLibrarySortYearValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{pg: &cs.PG{}}
	r := gin.New()
	r.GET("/library", h.listLibrary)

	get := func(query string) (int, string) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/library"+query, nil)
		r.ServeHTTP(w, req)
		return w.Code, w.Body.String()
	}

	for _, tc := range []struct {
		query    string
		wantCode int
		wantBody string
	}{
		{"?sort=year", 400, "sort=year needs type"},
		{"?type=all&sort=year", 400, "sort=year needs type"},
		{"?type=movies&sort=year", 503, "unavailable"}, // validation passed, no DB in tests
		{"?type=series&sort=year", 503, "unavailable"},
		{"?sort=rating", 400, "sort must be recent, name or year"},
	} {
		code, body := get(tc.query)
		if code != tc.wantCode || !strings.Contains(body, tc.wantBody) {
			t.Errorf("%s: got %d %q, want %d containing %q", tc.query, code, body, tc.wantCode, tc.wantBody)
		}
	}
}

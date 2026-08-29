package library

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// ctxWithParams builds a gin context carrying only route params, which is all
// bindPosterArgs and bindStillArgs read.
func ctxWithParams(params gin.Params) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Params = params
	return c
}

// TestBindPosterArgsRejectsAnOutOfRangeWidth is the negative control for the
// clamp in bindPosterArgs.
//
// The width it parses reaches imaging.Resize(src, width, 0, Lanczos), which
// allocates width × height × 4 bytes with the height derived from the source
// aspect ratio. On an ordinary 500x750 poster a width of 20000 asks for a
// single 2.4 GB allocation, and /lib carries no auth middleware — so an
// unbounded width here is one anonymous GET against the pod's memory budget.
//
// Negative control: drop the poster_resolver.ValidateWidth call from
// bindPosterArgs and the "far above the ceiling" case must fail.
func TestBindPosterArgsRejectsAnOutOfRangeWidth(t *testing.T) {
	for _, tt := range []struct {
		name    string
		file    string
		wantErr bool
	}{
		{name: "at the floor", file: "32.jpg"},
		{name: "ordinary card width", file: "160.jpg"},
		{name: "at the ceiling", file: "1600.jpg"},
		{name: "just below the floor", file: "31.jpg", wantErr: true},
		{name: "just above the ceiling", file: "1601.jpg", wantErr: true},
		{name: "far above the ceiling", file: "20000.jpg", wantErr: true},
		{name: "the shape that would OOM the pod", file: "999999.jpg", wantErr: true},
		{name: "not a number", file: "abc.jpg", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handler{}
			c := ctxWithParams(gin.Params{
				{Key: "type", Value: "movie"},
				{Key: "imdb_id", Value: "tt0111161"},
				{Key: "file", Value: tt.file},
			})
			_, err := h.bindPosterArgs(c)
			if tt.wantErr && err == nil {
				t.Errorf("width %q was accepted; unbounded it reaches imaging.Resize", tt.file)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("width %q was rejected but is inside the supported range: %v", tt.file, err)
			}
		})
	}
}

// TestBindStillArgsRejectsAnOutOfRangeWidth covers the episode-still route,
// which reaches the same imaging.Resize through a separate binder. The two
// binders drifted apart once already — that is why this is asserted twice
// rather than once.
func TestBindStillArgsRejectsAnOutOfRangeWidth(t *testing.T) {
	for _, tt := range []struct {
		name    string
		file    string
		wantErr bool
	}{
		{name: "ordinary still width", file: "320.jpg"},
		{name: "at the ceiling", file: "1600.jpg"},
		{name: "far above the ceiling", file: "20000.jpg", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handler{}
			c := ctxWithParams(gin.Params{
				{Key: "video_id", Value: "tt0111161"},
				{Key: "season", Value: "1"},
				{Key: "episode", Value: "1"},
				{Key: "file", Value: tt.file},
			})
			_, err := h.bindStillArgs(c)
			if tt.wantErr && err == nil {
				t.Errorf("width %q was accepted; unbounded it reaches imaging.Resize", tt.file)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("width %q was rejected but is inside the supported range: %v", tt.file, err)
			}
		})
	}
}

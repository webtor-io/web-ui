package embed

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/models"
)

// The embed page's POST carries the declaration like the resource page's
// action forms do, through the same allowlist.
func TestEmbedPostReadsTheDeclaration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, c := range []struct {
		form url.Values
		want models.DecodeRequest
	}{
		{url.Values{"settings": {`{"magnet":"m"}`}}, models.DecodeRequest{}},
		{url.Values{"settings": {`{"magnet":"m"}`}, "decode": {"hevc10-2160,hevc10,x"}}, models.DecodeRequest{Decode: "hevc10,hevc10-2160"}},
		{url.Values{"settings": {`{"magnet":"m"}`}, "decode-fallback": {"no_frames"}, "decode-class": {"hevc8"}}, models.DecodeRequest{FallbackReason: "no_frames", FallbackClass: "hevc8"}},
	} {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		req := httptest.NewRequest(http.MethodPost, "/show?id=abc", strings.NewReader(c.form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx.Request = req
		args, err := (&Handler{}).bindPostArgs(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if args.Decode != c.want {
			t.Errorf("%v: %+v, want %+v", c.form, args.Decode, c.want)
		}
	}
}

// The player's stream restart (lib/player/stream-restart.js) posts
// purge=true: the job is run again, not replayed from the hour's cache that
// holds the dead session. Nothing else purges.
func TestEmbedPostReadsPurge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, c := range []struct {
		form url.Values
		want bool
	}{
		{url.Values{"settings": {`{"magnet":"m"}`}}, false},
		{url.Values{"settings": {`{"magnet":"m"}`}, "purge": {"true"}}, true},
		{url.Values{"settings": {`{"magnet":"m"}`}, "purge": {"1"}}, false},
	} {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		req := httptest.NewRequest(http.MethodPost, "/show?id=abc", strings.NewReader(c.form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx.Request = req
		args, err := (&Handler{}).bindPostArgs(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if args.Purge != c.want {
			t.Errorf("%v: purge %v, want %v", c.form, args.Purge, c.want)
		}
	}
}

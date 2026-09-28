package action

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/models"
)

// bindArgs runs bindPostArgs on a form behind the app's session middleware.
func bindArgs(t *testing.T, form url.Values) *PostArgs {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(sessions.Sessions("session", cookie.NewStore([]byte("test secret"))))
	var got *PostArgs
	r.POST("/stream-video", func(c *gin.Context) {
		a, err := (&Handler{}).bindPostArgs(c)
		if err != nil {
			t.Fatalf("bindPostArgs: %v", err)
		}
		got = a
	})
	req := httptest.NewRequest(http.MethodPost, "/stream-video", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(httptest.NewRecorder(), req)
	if got == nil {
		t.Fatal("handler did not run")
	}
	return got
}

// The declaration and the fallback fields come from the request, through
// the allowlist, onto the stream's user data.
func TestBindPostArgsReadsTheDeclaration(t *testing.T) {
	base := url.Values{"resource-id": {"08ada5a7a6183aae1e09d831df6748d566095a10"}, "item-id": {"item-1"}}
	if d := bindArgs(t, base).VideoStreamUserData.DecodeRequest; d != (models.DecodeRequest{}) {
		t.Errorf("no fields: %+v", d)
	}
	f := url.Values{"decode": {"hdr-pq,hevc10,bogus"}, "decode-fallback": {"decode_error"}, "decode-class": {"hevc10-2160"}}
	for k, v := range base {
		f[k] = v
	}
	want := models.DecodeRequest{Decode: "hevc10,hdr-pq", FallbackReason: "decode_error", FallbackClass: "hevc10-2160"}
	if d := bindArgs(t, f).VideoStreamUserData.DecodeRequest; d != want {
		t.Errorf("got %+v, want %+v", d, want)
	}
	f.Set("decode-fallback", "whatever")
	if d := bindArgs(t, f).VideoStreamUserData.DecodeRequest; d.FallbackReason != "" || d.FallbackClass != "" {
		t.Errorf("an unknown reason passed: %+v", d)
	}
}

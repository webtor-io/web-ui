package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/supertokens/supertokens-golang/recipe/session"
)

// The cookie is what the SDK writes from the front-token header, raw: std
// base64 with '+' and '/', which must not be query-unescaped on the way in.
func TestExpiredForReadsTheFrontToken(t *testing.T) {
	ate := time.Now().Add(-90 * time.Minute)
	v := session.BuildFrontToken("u", uint64(ate.UnixMilli()), map[string]any{"pad": "?>?>?>~~~"})
	if !strings.ContainsAny(v, "+/") {
		t.Fatalf("fixture %q has no '+' or '/', so it proves nothing about unescaping", v)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "sFrontToken", Value: v})
	d, ok := expiredFor(r)
	if !ok || d < 89*time.Minute || d > 91*time.Minute {
		t.Fatalf("expiredFor = %v, %v; want ~90m, true", d, ok)
	}
	for _, bad := range []string{"", "not-base64!", "e30="} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if bad != "" {
			r.AddCookie(&http.Cookie{Name: "sFrontToken", Value: bad})
		}
		if _, ok := expiredFor(r); ok {
			t.Errorf("expiredFor(%q) reported a value", bad)
		}
	}
}

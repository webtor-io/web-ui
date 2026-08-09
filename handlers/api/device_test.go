package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/services/libapi"
)

// The DB is deliberately absent in these tests: everything that guards the
// device endpoints BEFORE storage — binding, pacing, code-creation limits —
// must hold on its own, because it is what stands between an anonymous
// caller and the table.
func newDeviceTestServer() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &Handler{
		pg:                &cs.PG{},
		domain:            "https://example.com",
		deviceCodeLimiter: libapi.NewRateLimiterWith(0.05, 2),
		devicePollLimiter: libapi.NewRateLimiterWith(libapi.DevicePollRPS, 1),
	}
	r.POST(libapi.MountPath+"/device/code", h.deviceCode)
	r.POST(libapi.MountPath+"/device/token", h.deviceToken)
	return r
}

func postJSON(r *gin.Engine, path string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestDeviceTokenRequiresAValidDeviceCode(t *testing.T) {
	r := newDeviceTestServer()
	if w := postJSON(r, "/api/v1/device/token", `{}`); w.Code != 400 || errCode(t, w) != libapi.CodeBadRequest {
		t.Errorf("empty body: got %d %s", w.Code, w.Body.String())
	}
	if w := postJSON(r, "/api/v1/device/token", `{"device_code":"not-a-uuid"}`); w.Code != 400 || errCode(t, w) != libapi.CodeBadRequest {
		t.Errorf("malformed code: got %d %s", w.Code, w.Body.String())
	}
}

// Polling faster than the advertised interval answers slow_down before any
// storage work — the limiter IS the pacing contract from the spec.
func TestDeviceTokenPacesPolling(t *testing.T) {
	r := newDeviceTestServer()
	body := `{"device_code":"6c0b8bad-4b41-4bcb-9d10-4c0a0a8e1e3f"}`
	// First poll passes the limiter and dies on the absent DB — fine here.
	if w := postJSON(r, "/api/v1/device/token", body); errCode(t, w) == libapi.CodeSlowDown {
		t.Fatalf("first poll must not be slow_down")
	}
	if w := postJSON(r, "/api/v1/device/token", body); w.Code != 400 || errCode(t, w) != libapi.CodeSlowDown {
		t.Errorf("rapid second poll: got %d %s, want slow_down", w.Code, w.Body.String())
	}
}

func TestDeviceCodeIsRateLimitedPerIP(t *testing.T) {
	r := newDeviceTestServer()
	// Burst of 2 in the test limiter: the third request must be cut off
	// before it can touch anything.
	postJSON(r, "/api/v1/device/code", ``)
	postJSON(r, "/api/v1/device/code", ``)
	w := postJSON(r, "/api/v1/device/code", ``)
	if w.Code != 429 || errCode(t, w) != libapi.CodeRateLimited {
		t.Errorf("third code request: got %d %s, want 429 rate_limited", w.Code, w.Body.String())
	}
	if w.Header().Get("Retry-After") == "" {
		t.Errorf("429 without Retry-After")
	}
}

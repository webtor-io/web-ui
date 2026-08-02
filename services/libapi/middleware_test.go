package libapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	co "github.com/webtor-io/web-ui/services/common"
)

const (
	attackerKey = "aaaaaaaa-1111-2222-3333-444444444444"
	victimKey   = "bbbbbbbb-5555-6666-7777-888888888888"
)

// probeToken runs the API-key middleware over a request and reports which token
// the rest of the chain (services/access_token, and therefore every downstream
// user lookup) will authenticate as.
func probeToken(t *testing.T, req *http.Request) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterAPIKeyMiddleware(r, MountPath)
	var seen string
	r.GET(MountPath+"/*rest", func(c *gin.Context) {
		seen = c.Query(co.AccessTokenParamName)
		c.Status(http.StatusOK)
	})
	r.ServeHTTP(httptest.NewRecorder(), req)
	return seen
}

// The account a request acts as must come from the key header and nothing else.
// If a caller-supplied ?token= could win, anyone could read another account's
// library by naming its key in the query string — no key of their own required.
func TestSuppliedTokenCannotOverrideHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, MountPath+"/library?token="+victimKey, nil)
	req.Header.Set("Authorization", "Bearer "+attackerKey)

	if got := probeToken(t, req); got != attackerKey {
		t.Errorf("request authenticated as %q, want the key from the header %q", got, attackerKey)
	}
}

// Same for a request with no key at all: a bare ?token= must not authenticate
// anything on this endpoint.
func TestSuppliedTokenWithoutHeaderIsDropped(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, MountPath+"/library?token="+victimKey, nil)

	if got := probeToken(t, req); got != "" {
		t.Errorf("request authenticated as %q, want nothing", got)
	}
}

// A malformed key must arrive as "no key" rather than as a token the chain
// tries to parse — that path answers 500, and a typo in a curl command must not
// read as "webtor is broken".
func TestMalformedKeyIsNotForwarded(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, MountPath+"/library", nil)
	req.Header.Set("Authorization", "Bearer not-a-uuid")

	if got := probeToken(t, req); got != "" {
		t.Errorf("forwarded %q downstream, want nothing", got)
	}
}

func TestRequestAPIKey(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers map[string]string
		want    string
	}{
		{name: "bearer", headers: map[string]string{"Authorization": "Bearer " + attackerKey}, want: attackerKey},
		// RFC 7235 makes the scheme case-insensitive, and clients get it wrong.
		{name: "lowercase scheme", headers: map[string]string{"Authorization": "bearer " + attackerKey}, want: attackerKey},
		{name: "api key header", headers: map[string]string{"X-Api-Key": attackerKey}, want: attackerKey},
		{name: "basic is not a key", headers: map[string]string{"Authorization": "Basic Zm9vOmJhcg=="}, want: ""},
		{name: "none", headers: nil, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, MountPath+"/library", nil)
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			if got := RequestAPIKey(req); got != tc.want {
				t.Errorf("RequestAPIKey = %q, want %q", got, tc.want)
			}
		})
	}
}

// probeHost reports the path a request ends up routed to.
func probeHost(t *testing.T, host string, path string) (string, int) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterHostMiddleware(r, []string{"api.example.com"}, HostPrefix)
	var seen string
	r.GET(MountPath+"/*rest", func(c *gin.Context) {
		seen = c.Request.URL.Path
		c.Status(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = host
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return seen, w.Code
}

// A dedicated hostname serves the API at its root, with the version still in
// the path — api.example.com/v1/library and example.com/api/v1/library are one route.
func TestDedicatedHostKeepsTheVersion(t *testing.T) {
	got, code := probeHost(t, "api.example.com", "/"+Version+"/library")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if want := MountPath + "/library"; got != want {
		t.Errorf("routed to %q, want %q", got, want)
	}
}

// The Host header carries a port whenever the service is not on 443, and
// matching has to survive it.
func TestDedicatedHostIgnoresPort(t *testing.T) {
	if _, code := probeHost(t, "api.example.com:8080", "/"+Version+"/library"); code != http.StatusOK {
		t.Errorf("status = %d with a port in Host, want 200", code)
	}
}

// Another host must be left alone, or every page of the main site would be
// rewritten onto /api.
func TestOtherHostsAreUntouched(t *testing.T) {
	if _, code := probeHost(t, "example.com", "/"+Version+"/library"); code != http.StatusNotFound {
		t.Errorf("status = %d on an unrelated host, want 404", code)
	}
}

// The re-dispatched pass arrives already prefixed; prefixing again would loop.
func TestAlreadyPrefixedPathIsNotRewrittenTwice(t *testing.T) {
	got, code := probeHost(t, "api.example.com", MountPath+"/library")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if want := MountPath + "/library"; got != want {
		t.Errorf("routed to %q, want %q", got, want)
	}
}

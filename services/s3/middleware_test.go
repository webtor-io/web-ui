package s3

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	co "github.com/webtor-io/web-ui/services/common"
)

const (
	mountPath   = "/s3"
	attackerKey = "aaaaaaaa-1111-2222-3333-444444444444"
	victimKey   = "bbbbbbbb-5555-6666-7777-888888888888"
)

// probeToken runs the access-key middleware over a request and reports which
// token the rest of the chain (services/access_token, and therefore every
// downstream user lookup) will authenticate as.
func probeToken(t *testing.T, req *http.Request) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterAccessKeyMiddleware(r, mountPath)
	var seen string
	r.Match([]string{http.MethodGet}, mountPath+"/*rest", func(c *gin.Context) {
		seen = c.Query(co.AccessTokenParamName)
		c.Status(http.StatusOK)
	})
	r.ServeHTTP(httptest.NewRecorder(), req)
	return seen
}

func authHeader(key string) string {
	return "AWS4-HMAC-SHA256 Credential=" + key +
		"/20260801/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-date, Signature=deadbeef"
}

// The user a request acts as must come from the signature and nothing else.
// A caller-supplied ?token= used to win, which let anyone holding their own
// valid credentials read another account's library by naming its access key in
// the query string — the signature check downstream still passed, because it
// only validates the key in the Authorization header.
func TestSuppliedTokenCannotOverrideSignature(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, mountPath+"/all/?token="+victimKey, nil)
	req.Header.Set("Authorization", authHeader(attackerKey))

	if got := probeToken(t, req); got != attackerKey {
		t.Errorf("request authenticated as %q, want the signing key %q", got, attackerKey)
	}
}

// Same for an unsigned request: a bare ?token= must not authenticate anything
// on the S3 endpoint.
func TestSuppliedTokenWithoutSignatureIsDropped(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, mountPath+"/all/?token="+victimKey, nil)

	if got := probeToken(t, req); got != "" {
		t.Errorf("request authenticated as %q, want nothing", got)
	}
}

func TestSignatureKeyIsForwarded(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, mountPath+"/all/", nil)
	req.Header.Set("Authorization", authHeader(attackerKey))

	if got := probeToken(t, req); got != attackerKey {
		t.Errorf("got %q, want %q", got, attackerKey)
	}
}

// A presigned URL carries its credential in the query string.
func TestPresignedCredentialIsForwarded(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, mountPath+"/all/?X-Amz-Algorithm=AWS4-HMAC-SHA256"+
		"&X-Amz-Credential="+attackerKey+"%2F20260801%2Fus-east-1%2Fs3%2Faws4_request"+
		"&X-Amz-Date=20260801T120000Z&X-Amz-Expires=300&X-Amz-SignedHeaders=host&X-Amz-Signature=deadbeef", nil)

	if got := probeToken(t, req); got != attackerKey {
		t.Errorf("got %q, want %q", got, attackerKey)
	}
}

// A malformed access key must not reach the access-token middleware, which
// treats an unparseable token as a server error.
func TestMalformedAccessKeyIsDropped(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, mountPath+"/all/", nil)
	req.Header.Set("Authorization", authHeader("not-a-uuid"))

	if got := probeToken(t, req); got != "" {
		t.Errorf("got %q, want nothing", got)
	}
}

// A WebDAV client pointed at the S3 endpoint must be told which protocol it is
// speaking, not that its (nonexistent) access key is unknown.
func TestWrongProtocolIsNamed(t *testing.T) {
	for _, m := range []string{"PROPFIND", "PROPPATCH", "MKCOL", "LOCK"} {
		err := WrongProtocolError(m)
		if err == nil {
			t.Fatalf("%s: expected an error", m)
		}
		if err.Status != http.StatusMethodNotAllowed || err.Code != ErrCodeMethodNotAllowed {
			t.Errorf("%s: got %d/%s", m, err.Status, err.Code)
		}
		if !strings.Contains(err.Message, "WebDAV") {
			t.Errorf("%s: message does not mention WebDAV: %q", m, err.Message)
		}
	}
	for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete, "COPY", "MOVE"} {
		if err := WrongProtocolError(m); err != nil {
			t.Errorf("%s: must be allowed through, got %v", m, err)
		}
	}
}

// Requests outside the S3 endpoint must be left completely alone — the token
// param is a legitimate part of the WebDAV and Stremio flows.
func TestOtherPathsAreUntouched(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterAccessKeyMiddleware(r, mountPath)
	var seen string
	r.GET("/stremio/manifest.json", func(c *gin.Context) {
		seen = c.Query(co.AccessTokenParamName)
		c.Status(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/stremio/manifest.json?token="+victimKey, nil)
	req.Header.Set("Authorization", authHeader(attackerKey))
	r.ServeHTTP(httptest.NewRecorder(), req)

	if seen != victimKey {
		t.Errorf("got %q, want the untouched token %q", seen, victimKey)
	}
}

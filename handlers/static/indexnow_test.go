package static_test

import (
	"flag"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/urfave/cli"

	"github.com/webtor-io/web-ui/handlers/static"
	w "github.com/webtor-io/web-ui/services/web"
)

func serveWithIndexNowKey(t *testing.T, key string) (*gin.Engine, error) {
	t.Helper()
	t.Chdir("../..")
	gin.SetMode(gin.TestMode)
	set := flag.NewFlagSet("test", flag.ContinueOnError)
	set.String(static.AssetsPathFlag, t.TempDir(), "")
	set.String(static.AssetsHostFlag, "", "")
	set.String(static.IndexNowKeyFlag, key, "")
	r := gin.New()
	r.Use(w.NoindexDefault(false))
	return r, static.RegisterHandler(cli.NewContext(nil, set, nil), r)
}

func get(r *gin.Engine, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// The key file is what a search engine fetches to accept a submission for
// this host: the key itself, as text, at /<key>.txt.
func TestIndexNowKeyFileIsServed(t *testing.T) {
	const key = "0f3c9d6e2a8b47c1b5e9d2f7a4c6e8b1"
	r, err := serveWithIndexNowKey(t, key)
	if err != nil {
		t.Fatal(err)
	}
	rec := get(r, "/"+key+".txt")
	if rec.Code != http.StatusOK || rec.Body.String() != key {
		t.Fatalf("GET /%s.txt = %d %q, want 200 with the key", key, rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
	if rec := get(r, "/0000000000000000000000000000dead.txt"); rec.Code != http.StatusNotFound {
		t.Errorf("another key's file answers %d, want 404", rec.Code)
	}
}

// Without a key there is nothing to serve: a deployment that did not
// register a key must not answer for one.
func TestNoIndexNowKeyNoFile(t *testing.T) {
	r, err := serveWithIndexNowKey(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if rec := get(r, "/0f3c9d6e2a8b47c1b5e9d2f7a4c6e8b1.txt"); rec.Code != http.StatusNotFound {
		t.Errorf("GET of a key file without a key = %d, want 404", rec.Code)
	}
}

// A key the protocol rejects fails the start rather than serving a file
// no search engine accepts; a path character in it could also escape the
// route.
func TestInvalidIndexNowKeyFailsTheStart(t *testing.T) {
	for _, key := range []string{"short", "has/slash-0123456789", "has space 0123456789", strings.Repeat("a", 129)} {
		if _, err := serveWithIndexNowKey(t, key); err == nil {
			t.Errorf("key %q was accepted", key)
		}
	}
}

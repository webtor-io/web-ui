package library

import (
	"context"
	"flag"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	uuid "github.com/satori/go.uuid"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
)

// The add button of a private torrent the user has no record for says why
// rest-api refused it, and the refusal comes before the library is touched:
// the handler has no database, which would answer 500.
func TestAddButtonPrivateNoRecord(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	restAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/resource/"+hash {
			t.Errorf("unexpected %v %v", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"x","code":"private_torrent","reason":"no_record"}`))
	}))
	t.Cleanup(restAPI.Close)
	u, _ := url.Parse(restAPI.URL)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range api.RegisterFlags(nil) {
		f.Apply(fs)
	}
	if err := fs.Parse([]string{"--webtor-rest-api-host=" + u.Hostname(), "--webtor-rest-api-port=" + u.Port(),
		"--webtor-secret=test", "--rapidapi-host="}); err != nil {
		t.Fatal(err)
	}
	h := &Handler{api: api.New(cli.NewContext(cli.NewApp(), fs, nil), restAPI.Client()), pg: &cs.PG{}}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), auth.UserContext{}, &models.User{UserID: uuid.NewV4()})
		ctx = context.WithValue(ctx, api.ClaimsContext{}, &api.Claims{Uid: "76543210fedcba9876543210fedcba9876543210"})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	r.POST("/lib/add", h.add)
	req := httptest.NewRequest(http.MethodPost, "/lib/add", strings.NewReader("resource_id="+hash))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Return-Url", "/"+hash)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status %d, want 302: %s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "err=error.private_no_record") {
		t.Errorf("Location %q, want err=error.private_no_record", loc)
	}
}

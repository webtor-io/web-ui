package index

import (
	"context"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-contrib/multitemplate"
	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
	"github.com/urfave/cli"
	proto "github.com/webtor-io/claims-provider/proto"
	cs "github.com/webtor-io/common-services"

	"github.com/webtor-io/web-ui/handlers/common"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/models/pgtest"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/metrics"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/uploads"
	"github.com/webtor-io/web-ui/services/web"
)

// newHomeRouter serves the index routes with a stand-in index view that
// prints what the real one branches on, so the test reads status, headers
// and the error key without the whole template set. The template manager
// resolves "templates/" against the working directory, hence the chdir.
func newHomeRouter(t *testing.T) *gin.Engine {
	t.Helper()
	return homeRouter(t, `{{ define "main" }}[err={{ .ErrKey }}][args={{ with .ErrArgs }}{{ .Count }}/{{ .Full }}{{ end }}][instruction={{ .Data.Instruction }}]{{ end }}`, nil, nil)
}

// homeRouter serves the index routes with view as the index template; mw
// runs before them.
func homeRouter(t *testing.T, view string, pg *cs.PG, up *uploads.Service, mw ...gin.HandlerFunc) *gin.Engine {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"templates/layouts/main.html": `{{ template "main" . }}`,
		"templates/views/index.html":  view,
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "templates/partials"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(metrics.Middleware(), web.NoindexDefault(false))
	r.Use(mw...)
	re := multitemplate.NewRenderer()
	r.HTMLRender = re
	tm := template.NewManager[*web.Context](re)
	RegisterHandler(r, tm, pg, up)
	if err := tm.Init(); err != nil {
		t.Fatal(err)
	}
	return r
}

func get(r *gin.Engine, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
	return w
}

// The home page and the tool pages come back with ?err= after a failed form
// (web.RedirectWithErrorAndPath) and, until now, after every dead resource
// link. That URL is one visit's state: it must not be indexed, while the
// page itself stays indexable.
func TestHomeWithErrIsNoindex(t *testing.T) {
	r := newHomeRouter(t)
	tool := "/" + common.Tools[0].Url

	cases := []struct {
		target    string
		robots    string
		wantInBod string
	}{
		{"/", "index, follow", "[err=]"},
		{tool, "index, follow", "[err=]"},
		{"/?status=error&err=error.not_found&from=%2Fdeadbeef", "noindex", "[err=error.not_found]"},
		{tool + "?status=error&err=error.invalid_resource&from=%2F", "noindex", "[err=error.invalid_resource]"},
		// Not shown without status=error, but still an error-state URL.
		{"/?err=error.not_found", "noindex", "[err=]"},
		{"/?status=success&from=%2Fprofile", "index, follow", "[err=]"},
	}
	for _, tc := range cases {
		w := get(r, tc.target)
		if w.Code != http.StatusOK {
			t.Errorf("%s: status %d, want 200", tc.target, w.Code)
		}
		if got := w.Header().Get("X-Robots-Tag"); got != tc.robots {
			t.Errorf("%s: X-Robots-Tag %q, want %q", tc.target, got, tc.robots)
		}
		if !strings.Contains(w.Body.String(), tc.wantInBod) {
			t.Errorf("%s: body %q, want it to contain %q", tc.target, w.Body.String(), tc.wantInBod)
		}
	}
}

// error.hash_length comes back with its numbers next to ?err= (see
// web.RedirectWithErrorAndPath); the page reads them only alongside an error
// it shows, and only when they are numbers an infohash can have.
func TestHomeErrorCarriesItsNumbers(t *testing.T) {
	r := newHomeRouter(t)
	tool := "/" + common.Tools[0].Url
	cases := []struct{ target, want string }{
		{"/?status=error&err=error.hash_length&err_count=39&err_full=40&from=%2F", "[err=error.hash_length][args=39/40]"},
		{tool + "?status=error&err=error.hash_length&err_count=31&err_full=32&from=%2F", "[err=error.hash_length][args=31/32]"},
		{"/?status=error&err=error.free_text&from=%2F", "[err=error.free_text][args=]"},
		// forged or stray numbers are dropped
		{"/?status=error&err=error.hash_length&err_count=39&err_full=7&from=%2F", "[args=]"},
		{"/?status=error&err=error.hash_length&err_count=100000&err_full=40&from=%2F", "[args=]"},
		{"/?err_count=39&err_full=40", "[err=][args=]"},
	}
	for _, tc := range cases {
		w := get(r, tc.target)
		if !strings.Contains(w.Body.String(), tc.want) {
			t.Errorf("%s: body %q, want it to contain %q", tc.target, w.Body.String(), tc.want)
		}
	}
}

// The home page says where it spent its time before rendering, so a slow
// load can be split in DevTools without the logs.
func TestHomeReportsServerTiming(t *testing.T) {
	st := get(newHomeRouter(t), "/").Header().Get("Server-Timing")
	for _, part := range []string{"mw;dur=", "db;dur=", "ctx;dur="} {
		if !strings.Contains(st, part) {
			t.Errorf("Server-Timing %q lacks %q", st, part)
		}
	}
}

// The home page says how much of the upload quota the signed-in user holds,
// from the reserves alone; a guest, and an instance without uploads, get no
// line.
func TestHomeQuotaLine(t *testing.T) {
	const u0 = "9c5db5752b7c0123456789abcdef0123456789ab"
	db := pgtest.Start(t)
	var userID uuid.UUID
	if _, err := db.QueryOne(pg.Scan(&userID), `INSERT INTO "user" (email) VALUES (?) RETURNING user_id`, uuid.NewV4().String()+"@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO upload_reserve (user_id, resource_id, reserved_bytes, write_until) VALUES (?, ?, 3221225472, now() + interval '10 minutes')`, userID, u0); err != nil {
		t.Fatal(err)
	}
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range uploads.RegisterFlags(api.RegisterFlags(nil)) {
		f.Apply(fs)
	}
	if err := fs.Parse([]string{"--upload-url=https://upload.invalid", "--webtor-secret=test", "--rapidapi-host=", "--rapidapi-key="}); err != nil {
		t.Fatal(err)
	}
	c := cli.NewContext(cli.NewApp(), fs, nil)
	p := pgtest.CS(t, db)
	up := uploads.New(c, p, api.New(c, http.DefaultClient))
	if up == nil {
		t.Fatal("uploads are off")
	}
	free := &claims.Data{Context: &proto.Context{Tier: &proto.Tier{Id: 0}}}
	signIn := func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), auth.UserContext{}, &models.User{UserID: userID})
		c.Request = c.Request.WithContext(context.WithValue(ctx, claims.Context{}, free))
	}
	const view = `{{ define "main" }}[quota={{ with .Data.UploadQuota }}{{ .Used }}/{{ .Cap }}{{ end }}]{{ end }}`

	for _, tc := range []struct {
		name string
		r    func(t *testing.T) *gin.Engine
		want string
	}{
		{"signed in", func(t *testing.T) *gin.Engine { return homeRouter(t, view, p, up, signIn) }, "[quota=3221225472/10737418240]"},
		{"guest", func(t *testing.T) *gin.Engine { return homeRouter(t, view, p, up) }, "[quota=]"},
		{"uploads off", func(t *testing.T) *gin.Engine { return homeRouter(t, view, p, nil, signIn) }, "[quota=]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := get(tc.r(t), "/")
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), tc.want) {
				t.Errorf("status %d, body %q; want %q", w.Code, w.Body.String(), tc.want)
			}
		})
	}
}

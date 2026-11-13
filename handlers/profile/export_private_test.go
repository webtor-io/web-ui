package profile

import (
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
	"github.com/urfave/cli"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/models/pgtest"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
)

const exportTestHash = "0123456789abcdef0123456789abcdef01234567"

// newAccount inserts a user row and returns its id.
func newAccount(t *testing.T, db *pg.DB) uuid.UUID {
	t.Helper()
	var userID uuid.UUID
	if _, err := db.QueryOne(pg.Scan(&userID), `INSERT INTO "user" (email) VALUES (?) RETURNING user_id`, uuid.NewV4().String()+"@example.com"); err != nil {
		t.Fatal(err)
	}
	return userID
}

// restAPIFor is a client of a rest-api that answers the GET of userID's
// private tracker records with status and body; flags go to api.New after
// the stub's own.
func restAPIFor(t *testing.T, userID uuid.UUID, status int, body string, flags ...string) *api.Api {
	t.Helper()
	path := "/internal/private-trackers/" + api.GenerateSessionIDFromUser(&auth.User{ID: userID})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != path {
			t.Errorf("rest-api got %s %s, want GET %s", r.Method, r.URL.Path, path)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range api.RegisterFlags(nil) {
		f.Apply(fs)
	}
	if err := fs.Parse(append([]string{"--webtor-rest-api-host=" + u.Hostname(), "--webtor-rest-api-port=" + u.Port(),
		"--webtor-secret=test", "--rapidapi-host="}, flags...)); err != nil {
		t.Fatal(err)
	}
	return api.New(cli.NewContext(cli.NewApp(), fs, nil), srv.Client())
}

// getExport runs GET /profile/export signed in as userID.
func getExport(t *testing.T, db *pg.DB, userID uuid.UUID, restAPI *api.Api) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), auth.UserContext{}, &models.User{UserID: userID}))
	})
	r.GET("/profile/export", (&Handler{pg: pgtest.CS(t, db), restAPI: restAPI}).export)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/profile/export", nil))
	return w
}

// privateTrackersOf is the private_trackers of an export as it was written.
func privateTrackersOf(t *testing.T, w *httptest.ResponseRecorder) json.RawMessage {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", w.Code, w.Body.String())
	}
	var exp map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &exp); err != nil {
		t.Fatal(err)
	}
	return exp["private_trackers"]
}

// The export carries rest-api's records as hosts only: whatever else rest-api
// sends, a tracker URL with its key above all, stays out of the file. No
// records is an empty array, also where nothing is asked (RapidAPI).
func TestExportPrivateTrackersHostsOnly(t *testing.T) {
	db := pgtest.Start(t)

	userID := newAccount(t, db)
	w := getExport(t, db, userID, restAPIFor(t, userID, http.StatusOK, `[{"resource_id":"`+exportTestHash+`","hosts":["tracker.example"],`+
		`"source":"upload","created_at":"2026-11-01T10:00:00Z","updated_at":"2026-11-02T11:00:00Z",`+
		`"url":"http://tracker.example/announce?passkey=00000000000000000000000000000000"}]`))
	raw := privateTrackersOf(t, w)
	var records []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &records); err != nil || len(records) != 1 {
		t.Fatalf("private_trackers %s, want one record: %v", raw, err)
	}
	var keys []string
	for k := range records[0] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if got := strings.Join(keys, ","); got != "created_at,hosts,resource_id,source,updated_at" {
		t.Errorf("record keys %s, want created_at,hosts,resource_id,source,updated_at", got)
	}
	var hosts []string
	_ = json.Unmarshal(records[0]["hosts"], &hosts)
	if string(records[0]["resource_id"]) != `"`+exportTestHash+`"` || strings.Join(hosts, ",") != "tracker.example" {
		t.Errorf("record %s on %v, want %s on tracker.example", records[0]["resource_id"], hosts, exportTestHash)
	}
	if body := w.Body.String(); strings.Contains(body, "passkey") || strings.Contains(body, "http://tracker") {
		t.Errorf("export carries the tracker URL:\n%s", body)
	}

	for _, c := range []struct {
		name  string
		flags []string
	}{
		{"rest-api has none", nil},
		{"through RapidAPI", []string{"--rapidapi-host=gateway.invalid", "--rapidapi-key=0000"}},
	} {
		userID := newAccount(t, db)
		if got := string(privateTrackersOf(t, getExport(t, db, userID, restAPIFor(t, userID, http.StatusOK, `[]`, c.flags...)))); got != "[]" {
			t.Errorf("%s: private_trackers %q, want []", c.name, got)
		}
	}
}

// A failed read of the records gives no export at all, never one without
// them: no file, back to the profile with "try again". private_unavailable is
// a store without private records, not a failure.
func TestExportFailsClosed(t *testing.T) {
	db := pgtest.Start(t)

	userID := newAccount(t, db)
	w := getExport(t, db, userID, restAPIFor(t, userID, http.StatusInternalServerError, `{"error":"boom"}`))
	if cd := w.Header().Get("Content-Disposition"); cd != "" || strings.Contains(w.Body.String(), "schema_version") {
		t.Errorf("Content-Disposition %q, body %s; want no export", cd, w.Body.String())
	}
	loc, err := url.Parse(w.Header().Get("Location"))
	if err != nil || w.Code != http.StatusFound || loc.Path != "/profile" || loc.Query().Get("err") != "error.generic" {
		t.Errorf("got %d to %q, want 302 to /profile with err=error.generic", w.Code, w.Header().Get("Location"))
	}

	userID = newAccount(t, db)
	w = getExport(t, db, userID, restAPIFor(t, userID, http.StatusUnprocessableEntity, `{"error":"x","code":"private_unavailable"}`))
	if got := string(privateTrackersOf(t, w)); got != "[]" {
		t.Errorf("private_unavailable: private_trackers %q, want []", got)
	}
}

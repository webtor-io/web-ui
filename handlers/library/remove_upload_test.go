package library

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/models/pgtest"
	"github.com/webtor-io/web-ui/services/auth"
)

// "Remove" in the library takes no upload's row away: an upload is deleted as
// a whole on its page or in My files, or its files, its Vault copy and its
// quota would stay with no row to reach them. The rest of the selection goes,
// and the page says so as a success, adding that the own files stay in My
// files: nothing failed. Kinds other than 0 are no one's own upload and go
// like torrents.
func TestRemoveKeepsUploads(t *testing.T) {
	const (
		torrent = "0123456789abcdef0123456789abcdef01234567"
		upload  = "9c5db5752b7c0123456789abcdef0123456789ab"
		kind1   = "9c5db5752b7c1123456789abcdef0123456789ab"
	)
	db := pgtest.Start(t)
	for _, id := range []string{torrent, upload, kind1} {
		if _, err := db.Exec(`INSERT INTO torrent_resource (resource_id, name, file_count, size_bytes) VALUES (?, 'n', 1, 1)`, id); err != nil {
			t.Fatal(err)
		}
	}
	h := &Handler{pg: pgtest.CS(t, db)}

	for _, c := range []struct {
		name    string
		ids     []string
		gone    []string
		kept    []string
		message string
	}{
		{"torrent", []string{torrent}, []string{torrent}, []string{upload, kind1}, "toast.removedFromLibrary"},
		{"upload", []string{upload}, nil, []string{torrent, upload, kind1}, "library.uploadKept"},
		{"mixed", []string{torrent, upload}, []string{torrent}, []string{upload, kind1}, "library.uploadKept"},
		{"kind 1", []string{kind1}, []string{kind1}, []string{torrent, upload}, "toast.removedFromLibrary"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var uid uuid.UUID
			if _, err := db.QueryOne(pg.Scan(&uid), `INSERT INTO "user" (email) VALUES (?) RETURNING user_id`, uuid.NewV4().String()+"@example.com"); err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{torrent, upload, kind1} {
				if _, err := db.Exec(`INSERT INTO library (user_id, resource_id, name) VALUES (?, ?, 'n')`, uid, id); err != nil {
					t.Fatal(err)
				}
			}

			gin.SetMode(gin.TestMode)
			r := gin.New()
			r.Use(func(c *gin.Context) {
				ctx := context.WithValue(c.Request.Context(), auth.UserContext{}, &models.User{UserID: uid})
				c.Request = c.Request.WithContext(ctx)
				c.Next()
			})
			r.POST("/lib/remove", h.remove)
			req := httptest.NewRequest(http.MethodPost, "/lib/remove", strings.NewReader(url.Values{"resource_ids": {strings.Join(c.ids, ",")}}.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("X-Return-Url", "/lib/movies")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusFound {
				t.Fatalf("status %d, want a redirect", w.Code)
			}
			loc, err := url.Parse(w.Header().Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			q := loc.Query()
			if q.Get("status") != "success" || q.Get("message") != c.message || q.Has("err") {
				t.Errorf("redirect %s, want success with %s", loc, c.message)
			}
			for _, id := range c.gone {
				if l, err := models.GetLibraryByResourceID(context.Background(), db, uid, id); err != nil || l != nil {
					t.Errorf("%s: row %+v, err %v; want it removed", id, l, err)
				}
			}
			for _, id := range c.kept {
				if l, err := models.GetLibraryByResourceID(context.Background(), db, uid, id); err != nil || l == nil {
					t.Errorf("%s: row %+v, err %v; want it kept", id, l, err)
				}
			}
		})
	}
}

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/models/pgtest"
	"github.com/webtor-io/web-ui/services/auth"
)

// DELETE /library/{id} of one's own upload answers 409 and keeps its row and
// its quota reserve: an upload is deleted as a whole, on its page. Someone
// else's upload, or one not in the library, is a 404 as before.
func TestDeleteLibraryRefusesUpload(t *testing.T) {
	const (
		upload  = "9c5db5752b7c0123456789abcdef0123456789ab"
		torrent = "0123456789abcdef0123456789abcdef01234567"
	)
	db := pgtest.Start(t)
	newUser := func() uuid.UUID {
		var id uuid.UUID
		if _, err := db.QueryOne(pg.Scan(&id), `INSERT INTO "user" (email) VALUES (?) RETURNING user_id`, uuid.NewV4().String()+"@example.com"); err != nil {
			t.Fatal(err)
		}
		return id
	}
	owner, stranger := newUser(), newUser()
	for _, q := range []string{
		`INSERT INTO torrent_resource (resource_id, name, file_count, size_bytes) VALUES (?1, 'n', 0, 0)`,
		`INSERT INTO library (user_id, resource_id, name) VALUES (?0, ?1, 'n')`,
		`INSERT INTO upload_reserve (user_id, resource_id, reserved_bytes, write_until) VALUES (?0, ?1, 3221225472, now() + interval '10 minutes')`,
		`INSERT INTO torrent_resource (resource_id, name, file_count, size_bytes) VALUES (?2, 't', 1, 1)`,
		`INSERT INTO library (user_id, resource_id, name) VALUES (?0, ?2, 't')`,
	} {
		if _, err := db.Exec(q, owner, upload, torrent); err != nil {
			t.Fatal(err)
		}
	}
	h := &Handler{pg: pgtest.CS(t, db)}
	del := func(uid uuid.UUID, id string) *httptest.ResponseRecorder {
		gin.SetMode(gin.TestMode)
		r := gin.New()
		r.Use(func(c *gin.Context) {
			ctx := context.WithValue(c.Request.Context(), auth.UserContext{}, &models.User{UserID: uid})
			c.Request = c.Request.WithContext(ctx)
			c.Next()
		})
		r.DELETE("/library/:resource_id", h.deleteLibrary)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/library/"+id, nil))
		return w
	}
	inLibrary := func(uid uuid.UUID, id string) bool {
		l, err := models.GetLibraryByResourceID(context.Background(), db, uid, id)
		if err != nil {
			t.Fatal(err)
		}
		return l != nil
	}

	t.Run("upload", func(t *testing.T) {
		w := del(owner, upload)
		var res struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if w.Code != http.StatusConflict || json.Unmarshal(w.Body.Bytes(), &res) != nil || res.Error.Code != "conflict" {
			t.Fatalf("status %d %s, want 409 conflict", w.Code, w.Body.String())
		}
		if !inLibrary(owner, upload) {
			t.Error("the upload's library row is gone")
		}
		var reserved int64
		if _, err := db.QueryOne(pg.Scan(&reserved), `SELECT reserved_bytes FROM upload_reserve WHERE user_id = ? AND resource_id = ?`, owner, upload); err != nil || reserved != 3<<30 {
			t.Errorf("reserve %d, err %v; want %d kept", reserved, err, int64(3<<30))
		}
	})

	t.Run("torrent", func(t *testing.T) {
		if w := del(owner, torrent); w.Code != http.StatusNoContent {
			t.Fatalf("status %d %s, want 204", w.Code, w.Body.String())
		}
		if inLibrary(owner, torrent) {
			t.Error("the torrent's library row is still there")
		}
	})

	t.Run("not in the library", func(t *testing.T) {
		if w := del(stranger, upload); w.Code != http.StatusNotFound {
			t.Fatalf("status %d %s, want 404", w.Code, w.Body.String())
		}
	})
}

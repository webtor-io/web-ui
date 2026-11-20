package api

import (
	"context"
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

// Removing an upload's library row frees no quota: its reserve row stays, so
// the bytes on the seeder keep counting.
func TestDeleteLibraryKeepsUploadReserve(t *testing.T) {
	const id = "9c5db5752b7c0123456789abcdef0123456789ab"
	db := pgtest.Start(t)
	var userID uuid.UUID
	if _, err := db.QueryOne(pg.Scan(&userID), `INSERT INTO "user" (email) VALUES (?) RETURNING user_id`, uuid.NewV4().String()+"@example.com"); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO torrent_resource (resource_id, name, file_count, size_bytes) VALUES (?1, 'n', 0, 0)`,
		`INSERT INTO library (user_id, resource_id, name) VALUES (?0, ?1, 'n')`,
		`INSERT INTO upload_reserve (user_id, resource_id, reserved_bytes, write_until) VALUES (?0, ?1, 3221225472, now() + interval '10 minutes')`,
	} {
		if _, err := db.Exec(q, userID, id); err != nil {
			t.Fatal(err)
		}
	}
	h := &Handler{pg: pgtest.CS(t, db)}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), auth.UserContext{}, &models.User{UserID: userID})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	r.DELETE("/library/:resource_id", h.deleteLibrary)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/library/"+id, nil))

	if w.Code != http.StatusNoContent {
		t.Fatalf("status %d, want %d: %s", w.Code, http.StatusNoContent, w.Body.String())
	}
	if l, err := models.GetLibraryByResourceID(context.Background(), db, userID, id); err != nil || l != nil {
		t.Fatalf("library row %+v, err %v; want it removed", l, err)
	}
	var reserved int64
	if _, err := db.QueryOne(pg.Scan(&reserved), `SELECT reserved_bytes FROM upload_reserve WHERE user_id = ? AND resource_id = ?`, userID, id); err != nil {
		t.Fatalf("reserve row: %v", err)
	}
	if reserved != 3<<30 {
		t.Errorf("reserve %d, want %d", reserved, int64(3<<30))
	}
}

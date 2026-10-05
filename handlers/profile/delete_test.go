package profile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
	"github.com/supertokens/supertokens-golang/recipe/session/sessmodels"
	cs "github.com/webtor-io/common-services"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
)

// postDelete runs POST /profile/delete for a signed-in user. revoke stands in
// for the SuperTokens session (nil: no session, as on an instance without
// SuperTokens); the returned steps are revoke/delete in the order they ran.
func postDelete(t *testing.T, revoke func() error) (*httptest.ResponseRecorder, []string) {
	t.Helper()
	var steps []string
	orig := deleteUser
	deleteUser = func(context.Context, *pg.DB, uuid.UUID) error {
		steps = append(steps, "delete")
		return nil
	}
	t.Cleanup(func() { deleteUser = orig })

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), auth.UserContext{}, &models.User{UserID: uuid.NewV4()})
		if revoke != nil {
			sess := &sessmodels.TypeSessionContainer{RevokeSession: func() error {
				steps = append(steps, "revoke")
				return revoke()
			}}
			ctx = context.WithValue(ctx, sessmodels.SessionContext, sessmodels.SessionContainer(sess))
		}
		c.Request = c.Request.WithContext(ctx)
	})
	r.POST("/profile/delete", (&Handler{pg: &cs.PG{}}).delete)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/profile/delete", nil))
	return w, steps
}

// TestDeleteEndsTheSessionFirst: the /logout the deletion redirects to used
// to carry the live session, and the auth middleware resolved it by email
// into a fresh empty account, so deleting an account recreated it at once.
func TestDeleteEndsTheSessionFirst(t *testing.T) {
	w, steps := postDelete(t, func() error { return nil })
	if len(steps) != 2 || steps[0] != "revoke" || steps[1] != "delete" {
		t.Fatalf("steps %v, want [revoke delete]", steps)
	}
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/logout" {
		t.Fatalf("got %d to %q, want 302 to /logout", w.Code, w.Header().Get("Location"))
	}
}

// A session that could not be ended keeps the account: deleting it anyway
// would bring back the recreation.
func TestDeleteKeepsTheAccountWhenTheSessionStays(t *testing.T) {
	_, steps := postDelete(t, func() error { return context.DeadlineExceeded })
	if len(steps) != 1 || steps[0] != "revoke" {
		t.Fatalf("steps %v, want [revoke] only", steps)
	}
}

func TestDeleteWithoutSuperTokensSession(t *testing.T) {
	w, steps := postDelete(t, nil)
	if len(steps) != 1 || steps[0] != "delete" || w.Header().Get("Location") != "/logout" {
		t.Fatalf("steps %v, location %q, want [delete] and /logout", steps, w.Header().Get("Location"))
	}
}

package profile

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
	"github.com/supertokens/supertokens-golang/recipe/session/sessmodels"
	cs "github.com/webtor-io/common-services"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/vault"
)

// deleteTestUser is the account postDelete deletes.
var deleteTestUser = uuid.FromStringOrNil("01234567-89ab-cdef-0123-456789abcdef")

// postDelete runs POST /profile/delete for deleteTestUser, signed in. revoke
// stands in for the SuperTokens session (nil: no session, as on an instance
// without SuperTokens); recordsErr and vaultErr are what rest-api's deletion
// of the private tracker records and Vault's pledger lists answer. The
// returned steps are private-records/vault-uids/revoke/delete in the order
// they ran, and uid is the one rest-api was asked to delete the records of.
func postDelete(t *testing.T, revoke func() error, recordsErr, vaultErr error) (w *httptest.ResponseRecorder, steps []string, uid string) {
	t.Helper()
	origRecords, origVault, origDelete := deletePrivateRecords, forgetVaultPledger, deleteUser
	deletePrivateRecords = func(_ context.Context, _ *api.Api, id string) error {
		steps = append(steps, "private-records")
		uid = id
		return recordsErr
	}
	forgetVaultPledger = func(_ context.Context, _ *vault.Vault, userID uuid.UUID) error {
		steps = append(steps, "vault-uids")
		if userID != deleteTestUser {
			t.Errorf("vault asked to forget %v, want %v", userID, deleteTestUser)
		}
		return vaultErr
	}
	deleteUser = func(context.Context, *pg.DB, uuid.UUID) error {
		steps = append(steps, "delete")
		return nil
	}
	t.Cleanup(func() { deletePrivateRecords, forgetVaultPledger, deleteUser = origRecords, origVault, origDelete })

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), auth.UserContext{}, &models.User{UserID: deleteTestUser})
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
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/profile/delete", nil))
	return w, steps, uid
}

// TestDeleteEndsTheSessionFirst: the /logout the deletion redirects to used
// to carry the live session, and the auth middleware resolved it by email
// into a fresh empty account, so deleting an account recreated it at once.
func TestDeleteEndsTheSessionFirst(t *testing.T) {
	w, steps, _ := postDelete(t, func() error { return nil }, nil, nil)
	if got := strings.Join(steps, " "); got != "private-records vault-uids revoke delete" {
		t.Fatalf("steps [%s], want [private-records vault-uids revoke delete]", got)
	}
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/logout" {
		t.Fatalf("got %d to %q, want 302 to /logout", w.Code, w.Header().Get("Location"))
	}
}

// A session that could not be ended keeps the account: deleting it anyway
// would bring back the recreation.
func TestDeleteKeepsTheAccountWhenTheSessionStays(t *testing.T) {
	_, steps, _ := postDelete(t, func() error { return context.DeadlineExceeded }, nil, nil)
	if got := strings.Join(steps, " "); got != "private-records vault-uids revoke" {
		t.Fatalf("steps [%s], want [private-records vault-uids revoke] only", got)
	}
}

func TestDeleteWithoutSuperTokensSession(t *testing.T) {
	w, steps, _ := postDelete(t, nil, nil, nil)
	if got := strings.Join(steps, " "); got != "private-records vault-uids delete" || w.Header().Get("Location") != "/logout" {
		t.Fatalf("steps [%s], location %q, want [private-records vault-uids delete] and /logout", got, w.Header().Get("Location"))
	}
}

// The account's private tracker records go first, through rest-api under
// the account's site uid, while the account still exists to retry from; then
// its uid comes off Vault's pledger lists; only then do the session and the
// row go.
func TestDeleteRemovesPrivateRecordsFirst(t *testing.T) {
	w, steps, uid := postDelete(t, func() error { return nil }, nil, nil)
	if got := strings.Join(steps, " "); got != "private-records vault-uids revoke delete" {
		t.Fatalf("steps [%s], want [private-records vault-uids revoke delete]", got)
	}
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/logout" {
		t.Fatalf("got %d to %q, want 302 to /logout", w.Code, w.Header().Get("Location"))
	}
	if want := api.GenerateSessionIDFromUser(&auth.User{ID: deleteTestUser}); uid != want {
		t.Fatalf("rest-api asked to delete the records of %q, want %q", uid, want)
	}
}

// assertRetry: the deletion stopped with "try again", not on /logout.
func assertRetry(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	loc := w.Header().Get("Location")
	u, err := url.Parse(loc)
	if w.Code != http.StatusFound || err != nil || u.Path == "/logout" || u.Query().Get("err") != "error.generic" {
		t.Fatalf("got %d to %q, want a redirect with err=error.generic, not /logout", w.Code, loc)
	}
}

// rest-api failing to delete the records -- an old rest-api's 404, a refused
// internal claim -- keeps the session and the account: deleting it anyway
// leaves the records behind with nobody left to delete them. The text is
// "try again" whatever the cause: a bare 403 would otherwise read as a
// rights-holder block.
func TestDeleteAbortsOnRestAPIError(t *testing.T) {
	w, steps, _ := postDelete(t, func() error { return nil }, fmt.Errorf("%w url=/internal/private-trackers", api.ErrForbidden), nil)
	if got := strings.Join(steps, " "); got != "private-records" {
		t.Fatalf("steps [%s], want [private-records] only", got)
	}
	assertRetry(t, w)
}

// Vault failing to take the uid off a pledger list keeps the account too:
// the next try sends the lists again.
func TestDeleteAbortsOnVaultError(t *testing.T) {
	w, steps, _ := postDelete(t, func() error { return nil }, nil, errors.New("failed to remove the pledger of 0123456789abcdef0123456789abcdef01234567 from vault: connection refused"))
	if got := strings.Join(steps, " "); got != "private-records vault-uids" {
		t.Fatalf("steps [%s], want [private-records vault-uids] only", got)
	}
	assertRetry(t, w)
}

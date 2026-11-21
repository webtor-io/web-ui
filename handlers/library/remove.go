package library

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/uploads"
	"github.com/webtor-io/web-ui/services/web"
)

// remove takes one resource_id (the resource page's button) or several (the
// library page's "remove selected", partials/bulk_select.html).
func (s *Handler) remove(c *gin.Context) {
	u := auth.GetUserFromContext(c)
	if !u.HasAuth() {
		c.Status(http.StatusForbidden)
		return
	}
	rIDs, err := web.FormBulkIDs(c)
	kept := 0
	if err == nil {
		kept, err = s.removeFromLibrary(c.Request.Context(), u, rIDs)
	}
	// A redirect, not an abort: the forms post async into an element (main,
	// #library-button), and an aborted request answers an empty body that
	// lib/async.js swaps in -- a blank page. The library page shows ?err=.
	if err != nil {
		web.RedirectWithError(c, errors.Wrap(err, "failed to remove from library"))
		return
	}
	if kept > 0 {
		web.RedirectWithError(c, web.NewUserError("library.uploadKept", errors.New("uploads are not removed from the library")))
		return
	}
	web.RedirectWithSuccessAndMessage(c, "toast.removedFromLibrary")
}

// removeFromLibrary removes the rows of rIDs but an upload's, and says how many
// uploads it kept. An upload is deleted as a whole, by "Delete upload": taking
// only its library row away would leave its files, its Vault copy and its
// quota with no link to them.
func (s *Handler) removeFromLibrary(ctx context.Context, u *auth.User, rIDs []string) (kept int, err error) {
	db := s.pg.Get()
	if db == nil {
		return 0, errors.New("no db")
	}
	ids := rIDs[:0]
	for _, id := range rIDs {
		if uploads.Owned(id) {
			kept++
			continue
		}
		ids = append(ids, id)
	}
	return kept, models.RemoveFromLibrary(ctx, db, u.ID, ids...)
}

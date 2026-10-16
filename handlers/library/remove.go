package library

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
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
	if err == nil {
		err = s.removeFromLibrary(c.Request.Context(), u, rIDs)
	}
	// A redirect, not an abort: the forms post async into an element (main,
	// #library-button), and an aborted request answers an empty body that
	// lib/async.js swaps in -- a blank page. The library page shows ?err=.
	if err != nil {
		web.RedirectWithError(c, errors.Wrap(err, "failed to remove from library"))
		return
	}
	web.RedirectWithSuccessAndMessage(c, "toast.removedFromLibrary")
}

func (s *Handler) removeFromLibrary(ctx context.Context, u *auth.User, rIDs []string) (err error) {
	db := s.pg.Get()
	if db == nil {
		return errors.New("no db")
	}
	return models.RemoveFromLibrary(ctx, db, u.ID, rIDs...)
}

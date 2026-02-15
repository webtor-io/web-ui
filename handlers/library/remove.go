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

func (s *Handler) remove(c *gin.Context) {
	u := auth.GetUserFromContext(c)
	if !u.HasAuth() {
		c.Status(http.StatusForbidden)
		return
	}
	ctx := c.Request.Context()
	err := s.removeFromLibrary(ctx, c, u)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	web.RedirectWithSuccessAndMessage(c, "Removed from library")
}

func (s *Handler) removeFromLibrary(ctx context.Context, c *gin.Context, u *auth.User) (err error) {
	rID, _ := c.GetPostForm("resource_id")
	db := s.pg.Get()
	if db == nil {
		return errors.New("no db")
	}
	return models.RemoveFromLibrary(ctx, db, u.ID, rID)
}

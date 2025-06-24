package library

import (
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"net/http"
)

func (s *Handler) remove(c *gin.Context) {
	u := auth.GetUserFromContext(c)
	if !u.HasAuth() {
		c.Status(http.StatusForbidden)
		return
	}
	err := s.removeFromLibrary(c, u)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.Redirect(http.StatusFound, c.GetHeader("X-Return-Url"))
}

func (s *Handler) removeFromLibrary(c *gin.Context, u *auth.User) (err error) {
	rID, _ := c.GetPostForm("resource_id")
	db := s.pg.Get()
	if db == nil {
		return errors.New("no db")
	}
	return models.RemoveFromLibrary(db, u.ID, rID)
}

package library

import (
	"github.com/anacrolix/torrent/metainfo"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/models"
	"io"
	"net/http"
)

func (s *Handler) add(c *gin.Context) {
	u := auth.GetUserFromContext(c)
	if !u.HasAuth() {
		c.Status(http.StatusForbidden)
		return
	}
	err := s.addTorrentToLibrary(c, u)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.Redirect(http.StatusFound, c.GetHeader("X-Return-Url"))
}

func (s *Handler) addTorrentToLibrary(c *gin.Context, u *auth.User) (err error) {
	uID, err := uuid.FromString(u.ID)
	if err != nil {
		return
	}
	clms := api.GetClaimsFromContext(c)
	ctx := c.Request.Context()
	rID, _ := c.GetPostForm("resource_id")
	body, err := s.api.GetTorrent(ctx, clms, rID)
	if err != nil {
		return
	}
	mi, err := metainfo.Load(body)
	if err != nil {
		return
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return
	}
	db := s.pg.Get()
	if db == nil {
		return errors.New("no db")
	}
	err = models.AddTorrentToLibrary(db, uID, rID, info)
	if err != nil {
		return
	}
	defer func(body io.ReadCloser) {
		_ = body.Close()
	}(body)
	return nil
}

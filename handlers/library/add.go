package library

import (
	"github.com/anacrolix/torrent/metainfo"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/web"
	"io"
	"net/http"
)

func (s *Handler) add(c *gin.Context) {
	u := auth.GetUserFromContext(c)
	if !u.HasAuth() {
		c.Status(http.StatusForbidden)
		return
	}
	err, rID := s.addTorrentToLibrary(c, u)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	_, _ = s.jobs.Enrich(web.NewContext(c), rID)
	c.Redirect(http.StatusFound, c.GetHeader("X-Return-Url"))
}

func (s *Handler) addTorrentToLibrary(c *gin.Context, u *auth.User) (err error, id string) {
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
		return errors.New("no db"), id
	}
	err = models.AddTorrentToLibrary(db, u.ID, rID, info)
	if err != nil {
		return
	}
	defer func(body io.ReadCloser) {
		_ = body.Close()
	}(body)
	return nil, rID
}

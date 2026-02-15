package library

import (
	"bytes"
	"io"
	"net/http"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/web"
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
	web.RedirectWithSuccessAndMessage(c, "Added to library")
}

func (s *Handler) addTorrentToLibrary(c *gin.Context, u *auth.User) (err error, id string) {
	clms := api.GetClaimsFromContext(c)
	ctx := c.Request.Context()
	rID, _ := c.GetPostForm("resource_id")
	t, err := s.api.GetTorrentCached(ctx, clms, rID)
	if err != nil {
		return
	}
	body := io.NopCloser(bytes.NewReader(t))
	defer func(body io.ReadCloser) {
		_ = body.Close()
	}(body)
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
	_, err = models.AddTorrentToLibrary(ctx, db, u.ID, rID, &info, "", int64(len(t)))
	if err != nil {
		return
	}

	return nil, rID
}

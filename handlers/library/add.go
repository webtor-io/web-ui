package library

import (
	"net/http"

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
	var pe *api.PrivateError
	if errors.As(err, &pe) {
		web.RedirectWithError(c, err)
		return
	}
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to add torrent to library"))
		return
	}
	_, _ = s.jobs.Enrich(web.NewContext(c), rID)
	web.RedirectWithSuccessAndMessage(c, "toast.addedToLibrary")
}

func (s *Handler) addTorrentToLibrary(c *gin.Context, u *auth.User) (err error, id string) {
	clms := api.GetClaimsFromContext(c)
	ctx := c.Request.Context()
	rID, _ := c.GetPostForm("resource_id")
	info, size, err := s.api.LibraryTorrent(ctx, clms, rID)
	if err != nil {
		return
	}
	if info == nil {
		return errors.New("torrent not found"), id
	}
	db := s.pg.Get()
	if db == nil {
		return errors.New("no db"), id
	}
	_, err = models.AddTorrentToLibrary(ctx, db, u.ID, rID, info, "", size)
	if err != nil {
		return
	}

	return nil, rID
}

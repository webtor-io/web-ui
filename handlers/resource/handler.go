package resource

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	j "github.com/webtor-io/web-ui/jobs"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/vault"
	"github.com/webtor-io/web-ui/services/web"
)

type Handler struct {
	api            *api.Api
	jobs           *j.Jobs
	tb             template.Builder[*web.Context]
	pg             *cs.PG
	vault          *vault.Vault
	useDirectLinks bool
}

func RegisterHandler(c *cli.Context, r *gin.Engine, tm *template.Manager[*web.Context], api *api.Api, jobs *j.Jobs, pg *cs.PG, v *vault.Vault) {
	helper := NewHelper()
	h := &Handler{
		api:            api,
		jobs:           jobs,
		tb:             tm.MustRegisterViews("resource/*").WithHelper(helper).WithLayout("main"),
		pg:             pg,
		vault:          v,
		useDirectLinks: c.BoolT(common.UseDirectLinks),
	}
	r.POST("/", h.post)
	r.GET("/:resource_id", func(c *gin.Context) {
		rid := c.Param("resource_id")
		if strings.HasPrefix(rid, "magnet") {
			h.post(c)
			return
		}
		if strings.HasSuffix(rid, ".torrent") {
			h.downloadTorrent(c)
			return
		}
		h.get(c)
	})
}

func (s *Handler) downloadTorrent(c *gin.Context) {
	resourceID := strings.TrimSuffix(c.Param("resource_id"), ".torrent")
	claims := api.GetClaimsFromContext(c)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	torrent, err := s.api.GetTorrentCached(ctx, claims, resourceID)
	if err != nil {
		_ = c.Error(errors.Wrap(err, "failed to get torrent"))
		c.String(http.StatusInternalServerError, "failed to get torrent")
		return
	}

	mi, err := metainfo.Load(bytes.NewReader(torrent))
	if err != nil {
		_ = c.Error(errors.Wrap(err, "failed to load torrent metainfo"))
		c.String(http.StatusInternalServerError, "failed to load torrent")
		return
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		_ = c.Error(errors.Wrap(err, "failed to unmarshal torrent metainfo"))
		c.String(http.StatusInternalServerError, "failed to parse torrent")
		return
	}

	filename := info.Name + ".torrent"
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Header("Content-Length", fmt.Sprintf("%d", len(torrent)))
	c.Data(http.StatusOK, "application/x-bittorrent", torrent)
}

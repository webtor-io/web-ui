package discover

import (
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/web"
)

type indexData struct {
	AddonUrls []string
}

type Handler struct {
	tb template.Builder[*web.Context]
	pg *cs.PG
}

func RegisterHandler(r *gin.Engine, tm *template.Manager[*web.Context], pg *cs.PG) {
	h := &Handler{
		tb: tm.MustRegisterViews("discover/*").WithLayout("main"),
		pg: pg,
	}
	r.GET("/discover", h.index)
}

func (h *Handler) index(c *gin.Context) {
	u := auth.GetUserFromContext(c)
	if !u.HasAuth() {
		v := url.Values{
			"from":       []string{"discover"},
			"return-url": []string{"/discover"},
		}
		c.Redirect(http.StatusFound, "/login?"+v.Encode())
		return
	}

	db := h.pg.Get()
	if db == nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.New("no db"))
		return
	}

	addons, err := models.GetUserStremioAddonUrls(c.Request.Context(), db, u.ID)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get addon urls"))
		return
	}

	urls := make([]string, len(addons))
	for i, a := range addons {
		urls[i] = a.Url
	}

	h.tb.Build("discover/index").HTML(http.StatusOK, web.NewContext(c).WithData(&indexData{
		AddonUrls: urls,
	}))
}

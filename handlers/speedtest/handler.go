package speedtest

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/template"
	w "github.com/webtor-io/web-ui/services/web"
)

type Handler struct {
	tb   template.Builder[*w.Context]
	sapi *api.Api
}

type Data struct {
	TierName  string
	RateLimit uint64
}

func RegisterHandler(r *gin.Engine, tm *template.Manager[*w.Context], sapi *api.Api) {
	h := &Handler{
		tb:   tm.MustRegisterViews("speedtest/*").WithLayout("main"),
		sapi: sapi,
	}
	r.GET("/speedtest", h.get)
	r.GET("/speedtest/url", h.getURL)
}

func (s *Handler) get(c *gin.Context) {
	ctx := w.NewContext(c)
	d := Data{
		TierName: "free",
	}
	if ctx.Claims != nil && ctx.Claims.Context != nil && ctx.Claims.Context.Tier != nil {
		d.TierName = ctx.Claims.Context.Tier.Name
	}
	if ctx.Claims != nil && ctx.Claims.Claims != nil && ctx.Claims.Claims.Connection != nil && ctx.Claims.Claims.Connection.Rate != nil {
		d.RateLimit = *ctx.Claims.Claims.Connection.Rate
	}
	s.tb.Build("speedtest/index").HTML(http.StatusOK, ctx.WithData(&d))
}

func (s *Handler) getURL(c *gin.Context) {
	ctx := w.NewContext(c)
	u, err := s.getSpeedtestURL(c, ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"url": u})
}

func (s *Handler) getSpeedtestURL(c *gin.Context, ctx *w.Context) (string, error) {
	return s.sapi.GetSpeedtestURL(c.Request.Context(), ctx.ApiClaims)
}

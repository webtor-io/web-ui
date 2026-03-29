package speedtest

import (
	"math"
	"net/http"
	"strconv"
	"strings"

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
	AutoStart bool
}

type QualityTier struct {
	Name     string
	MinSpeed float64
	Ok       bool
}

type Plan struct {
	Name      string
	Speed     int
	Label     string
	IsCurrent bool
	Supported bool
}

type ResultData struct {
	SpeedMbps    float64
	SpeedDisplay string
	TierName     string
	RateLimit    uint64
	Quality      []QualityTier
	Plans        []Plan
	RateLimited  bool
}

var qualityTiers = []struct {
	Name     string
	MinSpeed float64
}{
	{"480p SD", 2.25},
	{"720p HD", 4.5},
	{"1080p Full HD", 9},
	{"1080p High Bitrate", 15},
	{"4K Ultra HD", 37.5},
}

var plans = []struct {
	Name  string
	Speed int
	Label string
}{
	{"Free", 5, "5 Mbps"},
	{"Bronze", 20, "20 Mbps"},
	{"Silver", 50, "50 Mbps"},
	{"Gold", 100, "100 Mbps"},
}

func RegisterHandler(r *gin.Engine, tm *template.Manager[*w.Context], sapi *api.Api) {
	h := &Handler{
		tb:   tm.MustRegisterViews("speedtest/*").WithLayout("main"),
		sapi: sapi,
	}
	r.GET("/speedtest", h.get)
	r.POST("/speedtest", h.postResult)
	r.GET("/speedtest/url", h.getURL)
}

func (s *Handler) getTierData(ctx *w.Context) (string, uint64) {
	tierName := "free"
	var rateLimit uint64
	if ctx.Claims != nil && ctx.Claims.Context != nil && ctx.Claims.Context.Tier != nil {
		tierName = ctx.Claims.Context.Tier.Name
	}
	if ctx.Claims != nil && ctx.Claims.Claims != nil && ctx.Claims.Claims.Connection != nil && ctx.Claims.Claims.Connection.Rate != nil {
		rateLimit = *ctx.Claims.Claims.Connection.Rate
	}
	return tierName, rateLimit
}

func (s *Handler) get(c *gin.Context) {
	ctx := w.NewContext(c)
	tierName, rateLimit := s.getTierData(ctx)
	s.tb.Build("speedtest/index").HTML(http.StatusOK, ctx.WithData(&Data{
		TierName:  tierName,
		RateLimit: rateLimit,
		AutoStart: c.Request.URL.Query().Has("again"),
	}))
}

func (s *Handler) postResult(c *gin.Context) {
	ctx := w.NewContext(c)
	tierName, rateLimit := s.getTierData(ctx)

	speedStr := c.PostForm("speed")
	speedMbps, _ := strconv.ParseFloat(speedStr, 64)
	speedMbps = math.Round(speedMbps*10) / 10

	var quality []QualityTier
	for _, t := range qualityTiers {
		quality = append(quality, QualityTier{
			Name:     t.Name,
			MinSpeed: t.MinSpeed,
			Ok:       speedMbps >= t.MinSpeed,
		})
	}

	var planList []Plan
	for _, p := range plans {
		planList = append(planList, Plan{
			Name:      p.Name,
			Speed:     p.Speed,
			Label:     p.Label,
			IsCurrent: strings.EqualFold(p.Name, tierName),
			Supported: speedMbps >= float64(p.Speed),
		})
	}

	rateLimited := rateLimit > 0 && speedMbps >= float64(rateLimit)*0.9

	s.tb.Build("speedtest/result").HTML(http.StatusOK, ctx.WithData(&ResultData{
		SpeedMbps:    speedMbps,
		SpeedDisplay: strconv.FormatFloat(speedMbps, 'f', 1, 64),
		TierName:     tierName,
		RateLimit:    rateLimit,
		Quality:      quality,
		Plans:        planList,
		RateLimited:  rateLimited,
	}))
}

func (s *Handler) getURL(c *gin.Context) {
	ctx := w.NewContext(c)
	u, err := s.sapi.GetSpeedtestURL(c.Request.Context(), ctx.ApiClaims)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"url": u})
}

package speedtest

import (
	"context"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/template"
	w "github.com/webtor-io/web-ui/services/web"
)

type Handler struct {
	tb   template.Builder[*w.Context]
	sapi *api.Api
	pg   *cs.PG
}

type Data struct {
	TierName  string
	RateLimit uint64
	AutoStart bool
	URLs      []api.SpeedtestURL
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
	SpeedMbps           float64
	SpeedDisplay        string
	PremiumSpeedMbps    float64
	PremiumSpeedDisplay string
	HasPremium          bool
	SpeedBoost          string
	TierName            string
	RateLimit           uint64
	Quality             []QualityTier
	Plans               []Plan
	RateLimited         bool
	ShareURL            string
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

func RegisterHandler(r *gin.Engine, tm *template.Manager[*w.Context], sapi *api.Api, pg *cs.PG) {
	h := &Handler{
		tb:   tm.MustRegisterViews("speedtest/*").WithLayout("main"),
		sapi: sapi,
		pg:   pg,
	}
	r.GET("/speedtest", h.get)
	r.GET("/speedtest/:id", h.getShared)
	r.POST("/speedtest", h.postResult)
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
	var urls []api.SpeedtestURL
	u, err := s.sapi.GetSpeedtestURLs(c.Request.Context(), ctx.ApiClaims)
	if err != nil {
		log.WithError(err).Warn("failed to get speedtest urls")
	} else {
		urls = u
	}
	s.tb.Build("speedtest/index").HTML(http.StatusOK, ctx.WithData(&Data{
		TierName:  tierName,
		RateLimit: rateLimit,
		AutoStart: c.Request.URL.Query().Has("again"),
		URLs:      urls,
	}))
}

func (s *Handler) postResult(c *gin.Context) {
	ctx := w.NewContext(c)
	tierName, rateLimit := s.getTierData(ctx)

	speedStr := c.PostForm("speed")
	speedMbps, _ := strconv.ParseFloat(speedStr, 64)
	speedMbps = math.Round(speedMbps*10) / 10

	premiumStr := c.PostForm("premium-speed")
	premiumMbps, _ := strconv.ParseFloat(premiumStr, 64)
	premiumMbps = math.Round(premiumMbps*10) / 10

	// Save results to DB
	sessionID := s.saveResults(c, speedMbps, premiumMbps)

	var shareURL string
	if sessionID != "" {
		shareURL = "/speedtest/" + sessionID
	}

	data := s.buildResultData(speedMbps, premiumMbps, tierName, rateLimit, shareURL)

	s.tb.Build("speedtest/result").HTML(http.StatusOK, ctx.WithData(data))
}

func getRemoteAddress(c *gin.Context) string {
	if addr := c.Request.Header.Get(gin.PlatformCloudflare); addr != "" {
		return addr
	}
	return c.ClientIP()
}

func stripQueryParams(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func resolveDestIP(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	ips, err := net.LookupHost(host)
	if err != nil || len(ips) == 0 {
		return ""
	}
	return ips[0]
}

type measurement struct {
	speedMbps  float32
	requestURL string
	destType   string
	destIP     string
}

func (s *Handler) saveResults(c *gin.Context, speedMbps float64, premiumMbps float64) string {
	db := s.pg.Get()
	if db == nil {
		return ""
	}

	sourceIP := getRemoteAddress(c)

	var measurements []measurement

	if standardURL := c.PostForm("standard-url"); standardURL != "" && speedMbps > 0 {
		if destIP := resolveDestIP(standardURL); destIP != "" {
			measurements = append(measurements, measurement{
				speedMbps:  float32(speedMbps),
				requestURL: stripQueryParams(standardURL),
				destType:   "standard",
				destIP:     destIP,
			})
		}
	}

	if premiumURL := c.PostForm("premium-url"); premiumURL != "" && premiumMbps > 0 {
		if destIP := resolveDestIP(premiumURL); destIP != "" {
			measurements = append(measurements, measurement{
				speedMbps:  float32(premiumMbps),
				requestURL: stripQueryParams(premiumURL),
				destType:   "premium",
				destIP:     destIP,
			})
		}
	}

	if len(measurements) == 0 {
		return ""
	}

	sessionID := uuid.NewV4()

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	for _, m := range measurements {
		err := models.CreateSpeedtestResult(ctx, db, &models.SpeedtestResult{
			SourceIP:   sourceIP,
			DestIP:     m.destIP,
			SpeedMbps:  m.speedMbps,
			RequestURL: m.requestURL,
			DestType:   m.destType,
			SessionID:  sessionID,
		})
		if err != nil {
			log.WithError(err).Warn("failed to save speedtest result")
			return ""
		}
	}

	return sessionID.String()
}

func (s *Handler) buildResultData(speedMbps, premiumMbps float64, tierName string, rateLimit uint64, shareURL string) *ResultData {
	hasPremium := premiumMbps > 0

	bestSpeed := speedMbps
	if hasPremium && premiumMbps > bestSpeed {
		bestSpeed = premiumMbps
	}

	var quality []QualityTier
	for _, t := range qualityTiers {
		quality = append(quality, QualityTier{
			Name:     t.Name,
			MinSpeed: t.MinSpeed,
			Ok:       bestSpeed >= t.MinSpeed,
		})
	}

	var planList []Plan
	for _, p := range plans {
		planList = append(planList, Plan{
			Name:      p.Name,
			Speed:     p.Speed,
			Label:     p.Label,
			IsCurrent: strings.EqualFold(p.Name, tierName),
			Supported: bestSpeed >= float64(p.Speed),
		})
	}

	rateLimited := rateLimit > 0 && speedMbps >= float64(rateLimit)*0.9

	var speedBoost string
	if hasPremium && speedMbps > 0 {
		boost := premiumMbps / speedMbps
		if boost >= 1.1 {
			speedBoost = fmt.Sprintf("%.1fx", boost)
		}
	}

	return &ResultData{
		SpeedMbps:           speedMbps,
		SpeedDisplay:        strconv.FormatFloat(speedMbps, 'f', 1, 64),
		PremiumSpeedMbps:    premiumMbps,
		PremiumSpeedDisplay: strconv.FormatFloat(premiumMbps, 'f', 1, 64),
		HasPremium:          hasPremium,
		SpeedBoost:          speedBoost,
		TierName:            tierName,
		RateLimit:           rateLimit,
		Quality:             quality,
		Plans:               planList,
		RateLimited:         rateLimited,
		ShareURL:            shareURL,
	}
}

func (s *Handler) getShared(c *gin.Context) {
	ctx := w.NewContext(c)

	sessionID, err := uuid.FromString(c.Param("id"))
	if err != nil {
		c.String(http.StatusNotFound, "Not found")
		return
	}

	db := s.pg.Get()
	if db == nil {
		c.String(http.StatusInternalServerError, "Database unavailable")
		return
	}

	results, err := models.GetSpeedtestResultsBySessionID(c.Request.Context(), db, sessionID)
	if err != nil || len(results) == 0 {
		c.String(http.StatusNotFound, "Not found")
		return
	}

	var speedMbps, premiumMbps float64
	for _, r := range results {
		switch r.DestType {
		case "premium":
			premiumMbps = float64(r.SpeedMbps)
		default:
			speedMbps = float64(r.SpeedMbps)
		}
	}

	data := s.buildResultData(speedMbps, premiumMbps, "", 0, "/speedtest/"+sessionID.String())

	s.tb.Build("speedtest/result").HTML(http.StatusOK, ctx.WithData(data))
}

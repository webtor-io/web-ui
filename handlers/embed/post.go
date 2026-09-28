package embed

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/metrics"
	"github.com/webtor-io/web-ui/services/web"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/services/embed"
	"github.com/webtor-io/web-ui/services/job"
)

type PostArgs struct {
	ID            string
	EmbedSettings *models.EmbedSettings
	// Decode is the embed page's HEVC passthrough declaration and, on a
	// restart after a failed passthrough, why (models.DecodeRequest).
	Decode models.DecodeRequest
}

type PostData struct {
	ID             string
	EmbedSettings  *models.EmbedSettings
	DomainSettings *embed.DomainSettingsData
	Job            *job.Job
}

func (s *Handler) bindPostArgs(c *gin.Context) (*PostArgs, error) {
	rawSettings := c.PostForm("settings")
	var settings models.EmbedSettings
	err := json.Unmarshal([]byte(rawSettings), &settings)
	if err != nil {
		return nil, err
	}
	id := c.Query("id")

	return &PostArgs{
		ID:            id,
		EmbedSettings: &settings,
		Decode:        models.ParseDecodeRequest(c.PostForm("decode"), c.PostForm("decode-fallback"), c.PostForm("decode-class")),
	}, nil

}

func (s *Handler) post(c *gin.Context) {
	tpl := s.tb.Build("embed/post")
	pd := PostData{}
	args, err := s.bindPostArgs(c)
	if err != nil {
		tpl.HTML(http.StatusBadRequest, web.NewContext(c).WithData(pd).WithErr(err))
		return
	}
	pd.ID = args.ID
	u, err := url.Parse(args.EmbedSettings.Referer)
	if err != nil {
		tpl.HTML(http.StatusBadRequest, web.NewContext(c).WithData(pd).WithErr(err))
		return
	}
	domain := u.Hostname()
	dsd, err := s.ds.Get(domain)
	if err != nil {
		tpl.HTML(http.StatusBadRequest, web.NewContext(c).WithData(pd).WithErr(err))
		return
	}
	pd.EmbedSettings = args.EmbedSettings
	pd.DomainSettings = dsd
	uClaims := claims.GetFromContext(c)
	uSessionID := api.GenerateSessionID(c)
	if dsd.Claims != nil {
		uClaims = dsd.Claims
		uSessionID = dsd.SessionID
	}
	c, err = s.api.SetClaims(c, domain, uClaims, uSessionID)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to set embed claims"))
		return
	}
	if args.Decode.FallbackReason != "" {
		// A restart after a passthrough failed in this embed
		// (lib/player/passthrough.js restartEmbed): counted and logged as
		// on the site (handlers/action notePassthroughFallback).
		metrics.PassthroughFallback(args.Decode.FallbackReason, args.Decode.FallbackClass)
		log.WithFields(log.Fields{
			"reason": args.Decode.FallbackReason,
			"class":  args.Decode.FallbackClass,
			"embed":  domain,
		}).Info("passthrough fallback")
	}
	embedJob, err := s.jobs.Embed(web.NewContext(c), s.cl, args.EmbedSettings, dsd, args.Decode)
	if err != nil {
		tpl.HTML(http.StatusBadRequest, web.NewContext(c).WithData(pd).WithErr(err))
		return
	}
	pd.Job = embedJob
	tpl.HTML(http.StatusAccepted, web.NewContext(c).WithData(pd))
}

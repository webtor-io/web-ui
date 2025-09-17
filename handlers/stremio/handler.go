package stremio

import (
	"net/http"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	at "github.com/webtor-io/web-ui/services/access_token"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/stremio"
)

type Handler struct {
	at *at.AccessToken
	b  *stremio.Builder
}

func RegisterHandler(r *gin.Engine, at *at.AccessToken, b *stremio.Builder) {
	h := &Handler{
		at: at,
		b:  b,
	}

	gr := r.Group("/stremio")
	gr.GET("/manifest.json", h.manifest)
	gr.Use(auth.HasAuth)
	gr.Use(claims.IsPaid)
	gr.Use(cors.New(cors.Config{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"GET"},
	}))
	gr.POST("/url/generate", h.generateUrl)
	grapi := gr.Group("")
	grapi.Use(at.HasScope("stremio:read"))
	grapi.GET("/catalog/:type/*id", h.catalog)
	grapi.GET("/stream/:type/*id", h.stream)
	grapi.GET("/meta/:type/*id", h.meta)
}

func (s *Handler) generateUrl(c *gin.Context) {
	_, err := s.at.Generate(c, "stremio", []string{"stremio:read"})
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.Redirect(http.StatusFound, c.GetHeader("X-Return-Url"))
}

func (s *Handler) manifest(c *gin.Context) {
	mas, err := s.b.BuildManifestService()
	if err != nil {
		log.WithError(err).Error("failed to build manifest service")
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	resp, err := mas.GetManifest(c.Request.Context())
	if err != nil {
		log.WithError(err).Error("failed to get manifest response")
		_ = c.AbortWithError(http.StatusInternalServerError, err)
	}
	c.JSON(http.StatusOK, resp)
}

func (s *Handler) catalog(c *gin.Context) {
	ct := c.Param("type")
	user := auth.GetUserFromContext(c)
	cas, err := s.b.BuildCatalogService(user.ID)
	if err != nil {
		log.WithError(err).Error("failed to build catalog service")
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	resp, err := cas.GetCatalog(c.Request.Context(), ct)
	if err != nil {
		log.WithError(err).Error("failed to get catalog response")
		_ = c.AbortWithError(http.StatusInternalServerError, err)
	}
	c.JSON(http.StatusOK, resp)
}

func (s *Handler) meta(c *gin.Context) {
	ct := c.Param("type")
	id := s.cleanResourceID(c.Param("id"))
	user := auth.GetUserFromContext(c)
	mes, err := s.b.BuildMetaService(user.ID)
	if err != nil {
		log.WithError(err).Error("failed to build meta service")
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	resp, err := mes.GetMeta(c.Request.Context(), ct, id)
	if err != nil {
		log.WithError(err).Error("failed to get meta response")
		_ = c.AbortWithError(http.StatusInternalServerError, err)
	}
	c.JSON(http.StatusOK, resp)
}

func (s *Handler) stream(c *gin.Context) {
	ct := c.Param("type")
	id := s.cleanResourceID(c.Param("id"))
	user := auth.GetUserFromContext(c)
	cla := api.GetClaimsFromContext(c)
	sts, err := s.b.BuildStreamsService(user.ID, cla)
	if err != nil {
		log.WithError(err).Error("failed to build streams service")
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	resp, err := sts.GetStreams(c.Request.Context(), ct, id)
	if err != nil {
		log.WithError(err).Error("failed to get streams response")
		_ = c.AbortWithError(http.StatusInternalServerError, err)
	}
	c.JSON(http.StatusOK, resp)
}

func (s *Handler) cleanResourceID(rawID string) string {
	return strings.TrimPrefix(strings.TrimSuffix(rawID, ".json"), "/")
}

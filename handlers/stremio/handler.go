package stremio

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/dgrijalva/jwt-go"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	at "github.com/webtor-io/web-ui/services/access_token"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	sv "github.com/webtor-io/web-ui/services/common"
	lr "github.com/webtor-io/web-ui/services/link_resolver"
	"github.com/webtor-io/web-ui/services/stremio"
)

type Handler struct {
	at     *at.AccessToken
	b      *stremio.Builder
	pg     *cs.PG
	lr     *lr.LinkResolver
	secret string
}

func RegisterHandler(c *cli.Context, r *gin.Engine, at *at.AccessToken, b *stremio.Builder, pg *cs.PG, lr *lr.LinkResolver) {
	h := &Handler{
		at:     at,
		b:      b,
		pg:     pg,
		lr:     lr,
		secret: c.String(sv.SessionSecretFlag),
	}

	gr := r.Group("/stremio")
	gr.Use(cors.New(cors.Config{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"GET", "POST"},
	}))
	gr.GET("/manifest.json", h.manifest)
	// Public configure endpoint to satisfy stremio-addons directory requirements
	gr.GET("/configure", h.configure)
	gra := gr.Group("")
	gra.Use(auth.HasAuth)
	gra.POST("/url/generate", h.generateUrl)
	grapi := gra.Group("")
	grapi.Use(at.HasScope("stremio:read"))
	grapi.GET("/catalog/:type/*id", h.catalog)
	grapi.GET("/stream/:type/*id", h.stream)
	grapi.GET("/meta/:type/*id", h.meta)
	grapi.GET("/resolve/*data", h.resolve)
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
	user := auth.GetUserFromContext(c)
	hasToken := c.Query(sv.AccessTokenParamName) != ""
	mas, err := s.b.BuildManifestService(user, hasToken)
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
	cas, err := s.b.BuildCatalogService(user)
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
	mes, err := s.b.BuildMetaService(user)
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
	apiClaims := api.GetClaimsFromContext(c)
	cla := claims.GetFromContext(c)
	sts, err := s.b.BuildStreamsService(c.Request.Context(), user, s.lr, apiClaims, cla, c.Query(sv.AccessTokenParamName))
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

func (s *Handler) configure(c *gin.Context) {
	u := auth.GetUserFromContext(c)
	if !u.HasAuth() {
		v := url.Values{
			"from":       []string{"stremio-configure"},
			"return-url": []string{"/stremio/configure"},
		}
		c.Redirect(http.StatusFound, "/login?"+v.Encode())
		return
	}
	// For now, redirect authenticated users to their profile where the personalized addon URL and install link are shown
	c.Redirect(http.StatusFound, "/profile")
}

func (s *Handler) resolve(c *gin.Context) {
	// Step 1: Extract JWT token from URL path
	data := strings.TrimPrefix(c.Param("data"), "/")
	if data == "" {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	// Step 2: Parse and validate JWT token
	token, err := jwt.Parse(data, func(token *jwt.Token) (interface{}, error) {
		// Validate signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.secret), nil
	})

	if err != nil {
		log.WithError(err).Warn("failed to parse JWT token")
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	// Step 3: Extract claims
	jwtClaims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		log.Warn("invalid JWT token claims")
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	// Step 4: Extract hash and path from claims
	hash, ok := jwtClaims["hash"].(string)
	if !ok || hash == "" {
		log.Warn("missing or invalid hash in JWT claims")
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	path, ok := jwtClaims["path"].(string)
	if !ok || path == "" {
		log.Warn("missing or invalid path in JWT claims")
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	// Step 5: Get user context
	user := auth.GetUserFromContext(c)
	apiClaims := api.GetClaimsFromContext(c)
	userClaims := claims.GetFromContext(c)

	// Step 6: Resolve link using LinkResolver
	linkResult, err := s.lr.ResolveLink(c.Request.Context(), user.ID, apiClaims, userClaims, hash, path, true)
	if err != nil {
		log.WithError(err).
			WithField("hash", hash).
			WithField("path", path).
			Error("failed to resolve link")
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	// Step 7: Check if URL was generated
	if linkResult == nil || linkResult.URL == "" {
		log.WithField("hash", hash).
			WithField("path", path).
			Warn("no URL generated for resolve")
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	// Step 8: Redirect to destination URL
	c.Redirect(http.StatusFound, linkResult.URL)
}

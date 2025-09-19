package profile

import (
	"fmt"
	"net/http"

	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	at "github.com/webtor-io/web-ui/services/access_token"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/stremio"
	ua "github.com/webtor-io/web-ui/services/url_alias"
	"github.com/webtor-io/web-ui/services/web"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/services/template"
)

type Data struct {
	StremioAddonURL     string
	WebDAVURL           string
	EmbedDomains        []models.EmbedDomain
	AddonUrls           []models.StremioAddonUrl
	StremioSettings     *models.StremioSettingsData
	Is4KAvailable       bool
	MinBitrateFor4KMbps int64
	Error               string
}

type Handler struct {
	tb     template.Builder[*web.Context]
	ual    *ua.UrlAlias
	at     *at.AccessToken
	pg     *cs.PG
	claims *claims.Claims
}

func RegisterHandler(r *gin.Engine, tm *template.Manager[*web.Context], at *at.AccessToken, ual *ua.UrlAlias, pg *cs.PG, cl *claims.Claims) {
	h := &Handler{
		tb:     tm.MustRegisterViews("profile/*").WithLayout("main"),
		at:     at,
		ual:    ual,
		pg:     pg,
		claims: cl,
	}
	r.GET("/profile", h.get)
}

func (s *Handler) getStremioAddonURL(c *gin.Context) (string, error) {
	at, err := s.at.GetTokenByName(c, "stremio")
	if at == nil {
		return "", err
	}
	url := fmt.Sprintf("/%s/%s/stremio/", common.AccessTokenParamName, at.Token)

	al, err := s.ual.Get(url, false)
	if err != nil {
		return "", err
	}
	return al + "/manifest.json", nil

}

func (s *Handler) getWebDAVURL(c *gin.Context) (string, error) {
	at, err := s.at.GetTokenByName(c, "webdav")
	if at == nil {
		return "", err
	}
	url := fmt.Sprintf("/%s/%s/webdav/fs/", common.AccessTokenParamName, at.Token)

	al, err := s.ual.Get(url, true)
	if err != nil {
		return "", err
	}
	return al + "/webdav/", nil
}

func (s *Handler) get(c *gin.Context) {
	u := auth.GetUserFromContext(c)
	cla := claims.GetFromContext(c)
	if !u.HasAuth() {
		c.Redirect(http.StatusTemporaryRedirect, "/login")
		return
	}
	stremioURL, err := s.getStremioAddonURL(c)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	webdavURL, err := s.getWebDAVURL(c)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	// Get user domains
	db := s.pg.Get()
	domains, err := models.GetUserDomains(db, u.ID)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	// Get user addon URLs
	addonUrls, err := models.GetUserStremioAddonUrls(db, u.ID)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	// Get Stremio settings
	ss, err := stremio.GetUserSettingsDataByClaims(db, u.ID, cla)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	s.tb.Build("profile/get").HTML(http.StatusOK, web.NewContext(c).WithData(&Data{
		StremioAddonURL:     stremioURL,
		WebDAVURL:           webdavURL,
		EmbedDomains:        domains,
		AddonUrls:           addonUrls,
		StremioSettings:     ss,
		Is4KAvailable:       stremio.Is4KAvailable(cla),
		MinBitrateFor4KMbps: stremio.MinBitrateMBpsFor4K,
		Error:               c.Query("error"),
	}))
}

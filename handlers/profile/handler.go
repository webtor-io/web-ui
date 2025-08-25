package profile

import (
	"fmt"
	"net/http"

	at "github.com/webtor-io/web-ui/services/access_token"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/common"
	ua "github.com/webtor-io/web-ui/services/url_alias"
	"github.com/webtor-io/web-ui/services/web"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/services/template"
)

type Data struct {
	StremioAddonURL string
	WebDAVURL       string
}

type Handler struct {
	tb  template.Builder[*web.Context]
	ual *ua.UrlAlias
	at  *at.AccessToken
}

func RegisterHandler(r *gin.Engine, tm *template.Manager[*web.Context], at *at.AccessToken, ual *ua.UrlAlias) {
	h := &Handler{
		tb:  tm.MustRegisterViews("profile/*").WithLayout("main"),
		at:  at,
		ual: ual,
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
	s.tb.Build("profile/get").HTML(http.StatusOK, web.NewContext(c).WithData(&Data{
		StremioAddonURL: stremioURL,
		WebDAVURL:       webdavURL,
	}))
}

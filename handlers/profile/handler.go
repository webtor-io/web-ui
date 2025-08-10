package profile

import (
	"fmt"
	"github.com/webtor-io/web-ui/services"
	at "github.com/webtor-io/web-ui/services/access_token"
	"github.com/webtor-io/web-ui/services/auth"
	ua "github.com/webtor-io/web-ui/services/url_alias"
	"github.com/webtor-io/web-ui/services/web"
	"net/http"

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

// getServiceURL generates a URL for a specific service with token authentication
func (s *Handler) getServiceURL(c *gin.Context, serviceName, pathSuffix string) (string, error) {
	at, err := s.at.GetTokenByName(c, serviceName)
	if at == nil {
		return "", err
	}
	url := fmt.Sprintf("/%s/%s/%s/", services.AccessTokenParamName, at.Token, serviceName)

	al, err := s.ual.Get(url)
	if err != nil {
		return "", err
	}
	return al + pathSuffix, nil
}

func (s *Handler) getStremioAddonURL(c *gin.Context) (string, error) {
	return s.getServiceURL(c, "stremio", "/manifest.json")
}

func (s *Handler) getWebDAVURL(c *gin.Context) (string, error) {
	return s.getServiceURL(c, "webdav", "")
}

func (s *Handler) get(c *gin.Context) {
	u := auth.GetUserFromContext(c)
	if !u.HasAuth() {
		c.Redirect(http.StatusTemporaryRedirect, "/login")
		return
	}
	at, err := s.getStremioAddonURL(c)
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
		StremioAddonURL: at,
		WebDAVURL:       webdavURL,
	}))
}

package addon_url

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/stremio"
	"github.com/webtor-io/web-ui/services/web"
)

type Handler struct {
	pg        *cs.PG
	validator *stremio.AddonValidator
	domain    string
}

func RegisterHandler(c *cli.Context, av *stremio.AddonValidator, r *gin.Engine, pg *cs.PG) error {
	d := c.String(common.DomainFlag)
	if d != "" {
		u, err := url.Parse(d)
		if err != nil {
			return err
		}
		d = u.Hostname()
	}

	h := &Handler{
		pg:        pg,
		validator: av,
		domain:    d,
	}

	gr := r.Group("/addon-url")
	gr.Use(auth.HasAuth)
	gr.POST("/add", h.add)
	gr.POST("/delete/:id", h.delete)
	return nil
}

func (s *Handler) add(c *gin.Context) {
	addonUrl := strings.TrimSpace(c.PostForm("url"))
	user := auth.GetUserFromContext(c)
	err := s.addAddonUrl(addonUrl, user)
	if err != nil {
		log.WithError(err).Error("failed to add addon URL")
		web.RedirectWithError(c, err)
		return
	}
	c.Redirect(http.StatusFound, c.GetHeader("X-Return-Url"))
}

func (s *Handler) delete(c *gin.Context) {
	id := c.Param("id")
	user := auth.GetUserFromContext(c)
	err := s.deleteAddonUrl(id, user)
	if err != nil {
		log.WithError(err).Error("failed to delete addon URL")
		web.RedirectWithError(c, err)
		return
	}
	c.Redirect(http.StatusFound, c.GetHeader("X-Return-Url"))
}

func (s *Handler) addAddonUrl(addonUrl string, user *auth.User) (err error) {
	// Get URL from form data
	if addonUrl == "" {
		return errors.New("no addon URL provided")
	}

	// Validate URL format
	parsedUrl, err := url.Parse(addonUrl)
	if err != nil {
		return errors.New("invalid URL format")
	}

	// Ensure it's HTTP or HTTPS
	if parsedUrl.Scheme != "http" && parsedUrl.Scheme != "https" {
		return errors.New("URL must use http or https protocol")
	}

	// Ensure it ends with manifest.json for Stremio addons
	if !strings.HasSuffix(parsedUrl.Path, "/manifest.json") && !strings.HasSuffix(parsedUrl.Path, "manifest.json") {
		return errors.New("URL must point to a Stremio addon manifest.json file")
	}

	// Prevent users from adding Webtor's own manifest URL
	if s.domain != "" && (parsedUrl.Hostname() == s.domain || parsedUrl.Hostname() == "localhost" || parsedUrl.Hostname() == "127.0.0.1") {
		return errors.New("cannot add Webtor's own manifest URL")
	}

	// Validate addon URL availability and manifest structure
	if err := s.validator.ValidateURL(addonUrl); err != nil {
		return err
	}

	db := s.pg.Get()
	if db == nil {
		return errors.New("no db")
	}

	// Check current addon URL count for user
	currentCount, err := models.CountUserAddonUrls(db, user.ID)
	if err != nil {
		return
	}

	// Restrict to maximum 3 addon URLs (more than domains since they're just URLs)
	if currentCount >= 3 {
		return errors.New("maximum 3 addon URLs allowed")
	}

	// Check if URL already exists
	urlExists, err := models.AddonUrlExists(db, addonUrl)
	if err != nil {
		return
	}
	if urlExists {
		return errors.New("addon URL already exists")
	}

	// Create new addon URL
	return models.CreateAddonUrl(db, user.ID, addonUrl)
}

func (s *Handler) deleteAddonUrl(idStr string, user *auth.User) (err error) {
	id, err := uuid.FromString(idStr)
	if err != nil {
		return
	}

	db := s.pg.Get()
	if db == nil {
		return errors.New("no db")
	}

	// Delete addon URL owned by the current user
	return models.DeleteUserAddonUrl(db, id, user.ID)
}

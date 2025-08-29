package embed_domain

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
)

type Handler struct {
	pg     *cs.PG
	domain string
}

func RegisterHandler(c *cli.Context, r *gin.Engine, pg *cs.PG) error {
	d := c.String(common.DomainFlag)
	if d != "" {
		u, err := url.Parse(d)
		if err != nil {
			return err
		}
		d = u.Hostname()
	}

	h := &Handler{
		pg:     pg,
		domain: d,
	}

	gr := r.Group("/embed-domain")
	gr.Use(auth.HasAuth)
	gr.POST("/add", h.add)
	gr.POST("/delete/:id", h.delete)
	return nil
}

func (s *Handler) add(c *gin.Context) {
	domain := strings.TrimSpace(strings.ToLower(c.PostForm("domain")))
	user := auth.GetUserFromContext(c)
	err := s.addDomain(domain, user)
	if err != nil {
		log.WithError(err).Error("failed to add domain")
		c.Redirect(http.StatusFound, c.GetHeader("X-Return-Url"))
		return
	}
	c.Redirect(http.StatusFound, c.GetHeader("X-Return-Url"))
}

func (s *Handler) delete(c *gin.Context) {
	id := c.Param("id")
	user := auth.GetUserFromContext(c)
	err := s.deleteDomain(id, user)
	if err != nil {
		log.WithError(err).Error("failed to delete domain")
		c.Redirect(http.StatusFound, c.GetHeader("X-Return-Url"))
		return
	}
	c.Redirect(http.StatusFound, c.GetHeader("X-Return-Url"))
}

func (s *Handler) addDomain(domain string, user *auth.User) (err error) {
	// Get domain from form data
	if domain == "" {
		return errors.New("no domain provided")
	}

	// Remove protocol if present
	domain = strings.TrimPrefix(strings.TrimPrefix(domain, "http://"), "https://")

	// Remove trailing slash
	domain = strings.TrimSuffix(domain, "/")

	if domain == "localhost" || domain == "127.0.0.1" || domain == s.domain {
		return errors.New("localhost is not allowed")
	}

	db := s.pg.Get()

	if db == nil {
		return errors.New("no db")
	}

	// Check current domain count for user
	currentCount, err := models.CountUserDomains(db, user.ID)
	if err != nil {
		return
	}

	// Restrict to maximum 3 domains
	if currentCount >= 3 {
		return errors.New("maximum 3 domains allowed")
	}

	// Check if domain already exists
	domainExists, err := models.DomainExists(db, domain)
	if err != nil {
		return
	}
	if domainExists {
		return errors.New("domain already exists")
	}

	// Create new domain
	return models.CreateDomain(db, user.ID, domain)
}

func (s *Handler) deleteDomain(idStr string, user *auth.User) (err error) {
	id, err := uuid.FromString(idStr)
	if err != nil {
		return
	}

	db := s.pg.Get()
	if db == nil {
		return errors.New("no db")
	}

	// Delete domain owned by the current user
	return models.DeleteUserDomain(db, id, user.ID)
}

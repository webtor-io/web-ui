package embed_domain

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	uuid "github.com/satori/go.uuid"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
)

type Handler struct {
	pg *cs.PG
}

func RegisterHandler(r *gin.Engine, pg *cs.PG) {
	h := &Handler{
		pg: pg,
	}

	gr := r.Group("/embed-domain")
	gr.Use(auth.HasAuth)
	gr.Use(claims.HasEmbedDomains) // Require embed domains access (Claims.Embed.NoAds == true)
	gr.POST("/add", h.addDomain)
	gr.POST("/delete/:id", h.deleteDomain)
}

func (h *Handler) addDomain(c *gin.Context) {
	// Get domain from form data
	domain := strings.TrimSpace(strings.ToLower(c.PostForm("domain")))
	if domain == "" {
		c.Redirect(http.StatusFound, c.GetHeader("X-Return-Url"))
		return
	}

	// Remove protocol if present
	if strings.HasPrefix(domain, "http://") {
		domain = strings.TrimPrefix(domain, "http://")
	}
	if strings.HasPrefix(domain, "https://") {
		domain = strings.TrimPrefix(domain, "https://")
	}

	// Remove trailing slash
	domain = strings.TrimSuffix(domain, "/")

	user := auth.GetUserFromContext(c)
	db := h.pg.Get()

	// Check current domain count for user
	currentCount, err := models.CountUserDomains(db, user.ID)
	if err != nil {
		// Database error, redirect back
		c.Redirect(http.StatusFound, c.GetHeader("X-Return-Url"))
		return
	}

	// Restrict to maximum 3 domains
	if currentCount >= 3 {
		// User already has 3 domains, redirect back without adding
		c.Redirect(http.StatusFound, c.GetHeader("X-Return-Url"))
		return
	}

	// Check if domain already exists
	domainExists, err := models.DomainExists(db, domain)
	if err != nil {
		// Database error, redirect back
		c.Redirect(http.StatusFound, c.GetHeader("X-Return-Url"))
		return
	}
	if domainExists {
		// Domain already exists, just redirect back
		c.Redirect(http.StatusFound, c.GetHeader("X-Return-Url"))
		return
	}

	// Create new domain
	err = models.CreateDomain(db, user.ID, domain)
	if err != nil {
		// Failed to create, redirect back
		c.Redirect(http.StatusFound, c.GetHeader("X-Return-Url"))
		return
	}

	// Success, redirect back to profile
	c.Redirect(http.StatusFound, c.GetHeader("X-Return-Url"))
}

func (h *Handler) deleteDomain(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.FromString(idStr)
	if err != nil {
		c.Redirect(http.StatusFound, c.GetHeader("X-Return-Url"))
		return
	}

	user := auth.GetUserFromContext(c)
	db := h.pg.Get()

	// Delete domain owned by the current user
	err = models.DeleteUserDomain(db, id, user.ID)
	if err != nil {
		// Failed to delete, redirect back
		c.Redirect(http.StatusFound, c.GetHeader("X-Return-Url"))
		return
	}

	// Success, redirect back to profile
	c.Redirect(http.StatusFound, c.GetHeader("X-Return-Url"))
}

package discover

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/enrich"
	"github.com/webtor-io/web-ui/services/web"
)

// creditsView is enrich.Credits as the modal draws it: each cast photo as
// the proxied URL (web.PersonPhotoURL), not the TMDB file name. The cast is
// "people", not "cast": that key carried bare names before, and a bundle and
// a pod from either side of a rollout must not misread each other.
type creditsView struct {
	Directors []string   `json:"directors,omitempty"`
	Creators  []string   `json:"creators,omitempty"`
	People    []castView `json:"people,omitempty"`
}

type castView struct {
	Name  string `json:"name"`
	Photo string `json:"photo,omitempty"`
}

func newCreditsView(cr *enrich.Credits) creditsView {
	v := creditsView{Directors: cr.Directors, Creators: cr.Creators}
	for _, m := range cr.Cast {
		v.People = append(v.People, castView{Name: m.Name, Photo: web.PersonPhotoURL(m.Profile)})
	}
	return v
}

// credits is the handler for POST /discover/credits: directors / creators /
// cast from TMDB for the title open in the stream modal. Same request shape
// and response contract as reviews: 200 with a (possibly empty) object is a
// verdict the client may cache, 502 means the pipeline couldn't check.
func (h *Handler) credits(c *gin.Context) {
	if h.en == nil || !h.en.HasMappers() {
		c.JSON(http.StatusOK, creditsView{})
		return
	}
	var req reviewsRequest
	if err := c.ShouldBindJSON(&req); err != nil || !localizableID(req.ID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad payload"})
		return
	}
	ct := models.ContentTypeMovie
	if req.Type == "series" {
		ct = models.ContentTypeSeries
	}
	cr, err := h.en.CreditsByID(c.Request.Context(), req.ID, ct)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "credits lookup failed"})
		return
	}
	if cr == nil {
		cr = &enrich.Credits{}
	}
	c.JSON(http.StatusOK, newCreditsView(cr))
}

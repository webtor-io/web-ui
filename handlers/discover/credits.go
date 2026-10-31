package discover

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/enrich"
	"github.com/webtor-io/web-ui/services/i18n"
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
// cast from TMDB for the title open in the stream modal, names in the page
// language where known. Same request shape
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
	ctx, lang := c.Request.Context(), i18n.GetLang(c)
	// Localize first (a no-op in English): a modal opened before the grid's
	// /discover/localize finished shares its call (locCache), so the row
	// with names is stored before Credits reads it. Result and error unused.
	// Bounded like availability: the TMDB client has no timeout of its own,
	// and a cold id waits on a synchronous details call here.
	lctx, cancel := context.WithTimeout(ctx, availabilityTimeout)
	_, _, _ = h.en.LocalizeByID(lctx, req.ID, ct, lang)
	cancel()
	cr, err := h.en.CreditsByID(ctx, req.ID, ct, lang)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "credits lookup failed"})
		return
	}
	if cr == nil {
		cr = &enrich.Credits{}
	}
	c.JSON(http.StatusOK, newCreditsView(cr))
}

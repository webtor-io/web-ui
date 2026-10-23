package discover

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/enrich"
)

// credits is the handler for POST /discover/credits: directors / creators /
// cast from TMDB for the title open in the stream modal. Same request shape
// and response contract as reviews: 200 with a (possibly empty) object is a
// verdict the client may cache, 502 means the pipeline couldn't check.
func (h *Handler) credits(c *gin.Context) {
	if h.en == nil || !h.en.HasMappers() {
		c.JSON(http.StatusOK, enrich.Credits{})
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
	c.JSON(http.StatusOK, cr)
}

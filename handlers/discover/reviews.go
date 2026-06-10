package discover

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/enrich"
)

// reviewsEnricher is the slice of enrich.Enricher the reviews endpoint
// needs. Kept as an interface so the Level 2 worker is testable without
// a real mapper chain.
type reviewsEnricher interface {
	HasMappers() bool
	ReviewsByID(ctx context.Context, videoID string, ct models.ContentType) ([]enrich.Review, error)
}

type reviewsRequest struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type reviewView struct {
	Author    string   `json:"author,omitempty"`
	Rating    *float64 `json:"rating,omitempty"`
	Content   string   `json:"content"`
	URL       string   `json:"url,omitempty"`
	CreatedAt string   `json:"createdAt,omitempty"`
}

type reviewsResponse struct {
	Reviews []reviewView `json:"reviews"`
}

// reviews is the Level 1 handler for POST /discover/reviews. Single-id
// endpoint (the stream modal asks for one title at a time, unlike the
// batched grid localization). Response contract: 200 with a (possibly
// empty) reviews array is a definitive verdict the client may cache for
// the session; a 502 means the pipeline couldn't check and the client
// must leave the id uncached so a later modal open retries.
func (h *Handler) reviews(c *gin.Context) {
	if h.en == nil || !h.en.HasMappers() {
		c.JSON(http.StatusOK, reviewsResponse{Reviews: []reviewView{}})
		return
	}
	var req reviewsRequest
	if err := c.ShouldBindJSON(&req); err != nil || !localizableID(req.ID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad payload"})
		return
	}
	views, err := reviewsForID(c.Request.Context(), h.en, req.ID, req.Type)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "reviews lookup failed"})
		return
	}
	c.JSON(http.StatusOK, reviewsResponse{Reviews: views})
}

// reviewsForID is the Level 2 worker: maps the catalog type hint to a
// ContentType and runs the enrichment pipeline. nil-and-nil from
// ReviewsByID (id unknown to every provider even after MapByID) is a
// definitive "no reviews", not an error.
func reviewsForID(ctx context.Context, en reviewsEnricher, id string, typ string) ([]reviewView, error) {
	ct := models.ContentTypeMovie
	if typ == "series" {
		ct = models.ContentTypeSeries
	}
	revs, err := en.ReviewsByID(ctx, id, ct)
	if err != nil {
		return nil, err
	}
	views := make([]reviewView, 0, len(revs))
	for _, r := range revs {
		views = append(views, reviewView{
			Author:    r.Author,
			Rating:    r.Rating,
			Content:   r.Content,
			URL:       r.URL,
			CreatedAt: r.CreatedAt,
		})
	}
	return views, nil
}

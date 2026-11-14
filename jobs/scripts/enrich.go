package scripts

import (
	"context"

	"github.com/webtor-io/web-ui/services/enrich"
	"github.com/webtor-io/web-ui/services/job"
)

// EnrichScript carries no caller: enrichment reads as internal (see
// Enricher.retrieveTorrentItems), whoever started it.
type EnrichScript struct {
	enricher *enrich.Enricher
	rID      string
}

func NewEnrichScript(enricher *enrich.Enricher, rID string) *EnrichScript {
	return &EnrichScript{
		enricher: enricher,
		rID:      rID,
	}
}

func (s *EnrichScript) Run(ctx context.Context, j *job.Job) (err error) {
	return s.enricher.Enrich(ctx, s.rID, false, "")
}

func Enrich(enricher *enrich.Enricher, rID string) (job.Runnable, string) {
	return NewEnrichScript(enricher, rID), rID
}

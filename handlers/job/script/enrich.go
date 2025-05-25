package script

import (
	"context"
	"github.com/webtor-io/web-ui/services/enrich"
	"github.com/webtor-io/web-ui/services/job"
	"github.com/webtor-io/web-ui/services/web"
)

type EnrichScript struct {
	enricher *enrich.Enricher
	rID      string
	c        *web.Context
}

func NewEnrichScript(enricher *enrich.Enricher, c *web.Context, rID string) *EnrichScript {
	return &EnrichScript{
		enricher: enricher,
		rID:      rID,
		c:        c,
	}
}

func (s *EnrichScript) Run(ctx context.Context, j *job.Job) (err error) {
	return s.enricher.Enrich(ctx, s.rID, s.c.ApiClaims, false)
}

func Enrich(enricher *enrich.Enricher, c *web.Context, rID string) (job.Runnable, string) {
	return NewEnrichScript(enricher, c, rID), rID
}

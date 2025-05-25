package job

import (
	"context"
	"github.com/webtor-io/web-ui/handlers/job/script"
	"github.com/webtor-io/web-ui/services/web"
	"time"

	"github.com/webtor-io/web-ui/services/job"
)

func (s *Handler) Enrich(c *web.Context, rID string) (j *job.Job, err error) {
	es, hash := script.Enrich(s.enricher, c, rID)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	j = s.q.GetOrCreate("enrich").Enqueue(ctx, cancel, hash, job.NewScript(func(j *job.Job) (err error) {
		return es.Run(ctx, j)
	}), false)
	return
}

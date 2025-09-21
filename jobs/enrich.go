package j

import (
	"context"
	"time"

	"github.com/webtor-io/web-ui/jobs/scripts"
	"github.com/webtor-io/web-ui/services/web"

	"github.com/webtor-io/web-ui/services/job"
)

func (s *Jobs) Enrich(c *web.Context, rID string) (j *job.Job, err error) {
	es, hash := scripts.Enrich(s.enricher, c, rID)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	j = s.q.GetOrCreate("enrich").Enqueue(ctx, cancel, hash, job.NewScript(func(j *job.Job) (err error) {
		return es.Run(ctx, j)
	}), false)
	return
}

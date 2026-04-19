package j

import (
	"context"
	"crypto/sha1"
	"fmt"
	"time"

	"github.com/webtor-io/web-ui/jobs/scripts"
	"github.com/webtor-io/web-ui/services/web"

	"github.com/webtor-io/web-ui/services/job"
)

func (s *Jobs) Load(c *web.Context, args *scripts.LoadArgs) (j *job.Job, err error) {
	ls, hash, err := scripts.Load(s.api, s.i18n, c, args)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	id := fmt.Sprintf("%x", sha1.Sum([]byte(hash+"/"+c.Lang)))
	j = s.q.GetOrCreate("load").Enqueue(ctx, cancel, id, job.NewScript(func(j *job.Job) (err error) {
		err = ls.Run(ctx, j)
		if err != nil {
			return
		}
		rID := j.Context.Value("respID").(string)
		if s.enricher.HasMappers() {
			j.InProgress(s.T(c, "job.enrichingContent"))
			enrichErr := s.enricher.Enrich(ctx, rID, c.ApiClaims, false, args.HintVideoID)
			if enrichErr != nil {
				j.Warn(enrichErr)
			} else {
				j.Done()
			}
		}
		j.Redirect(web.LangURL(c.Lang, "/"+rID), s.T(c, "job.redirecting"))
		return
	}), false, s.errorFormatter(c))
	return
}

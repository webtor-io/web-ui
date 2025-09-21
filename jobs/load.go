package j

import (
	"context"
	"time"

	"github.com/webtor-io/web-ui/jobs/scripts"
	"github.com/webtor-io/web-ui/services/web"

	"github.com/webtor-io/web-ui/services/job"
)

func (s *Jobs) Load(c *web.Context, args *scripts.LoadArgs) (j *job.Job, err error) {
	ls, hash, err := scripts.Load(s.api, c, args)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	j = s.q.GetOrCreate("load").Enqueue(ctx, cancel, hash, job.NewScript(func(j *job.Job) (err error) {
		err = ls.Run(ctx, j)
		if err != nil {
			return
		}
		j.Redirect("/" + j.Context.Value("respID").(string))
		return
	}), false)
	return
}

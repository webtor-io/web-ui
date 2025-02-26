package job

import (
	"context"
	"github.com/webtor-io/web-ui/handlers/job/script"
	"github.com/webtor-io/web-ui/services/web"
	"time"

	"github.com/webtor-io/web-ui/services/job"
)

func (s *Handler) Load(c *web.Context, args *script.LoadArgs) (j *job.Job, err error) {
	ls, hash, err := script.Load(s.api, c, args)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
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

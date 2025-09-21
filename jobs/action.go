package j

import (
	"context"
	"time"

	"github.com/webtor-io/web-ui/jobs/scripts"
	m "github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/web"

	"github.com/webtor-io/web-ui/services/job"
)

func (s *Jobs) Action(c *web.Context, resourceID string, itemID string, action string, settings *m.StreamSettings, purge bool, vsud *m.VideoStreamUserData) (j *job.Job, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	as, id := scripts.Action(s.tb, s.api, c, resourceID, itemID, action, settings, nil, vsud, s.warmupTimeoutMin)
	j = s.q.GetOrCreate(action).Enqueue(ctx, cancel, id, as, purge)
	return
}

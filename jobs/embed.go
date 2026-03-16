package j

import (
	"context"
	"net/http"
	"time"

	"github.com/webtor-io/web-ui/jobs/scripts"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/embed"
	"github.com/webtor-io/web-ui/services/web"

	"github.com/webtor-io/web-ui/services/job"
)

func (s *Jobs) Embed(c *web.Context, cl *http.Client, settings *models.EmbedSettings, dsd *embed.DomainSettingsData) (j *job.Job, err error) {
	es, hash, err := scripts.Embed(s.tb, cl, c, s.api, settings, "", dsd, s.warmupTimeoutMin)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	j = s.q.GetOrCreate("embded").Enqueue(ctx, cancel, hash, es, false)
	return
}

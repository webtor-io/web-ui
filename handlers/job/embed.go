package job

import (
	"context"
	"github.com/webtor-io/web-ui/handlers/job/script"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/embed"
	"github.com/webtor-io/web-ui/services/web"
	"net/http"
	"time"

	"github.com/webtor-io/web-ui/services/job"
)

func (s *Handler) Embed(c *web.Context, cl *http.Client, settings *models.EmbedSettings, dsd *embed.DomainSettingsData) (j *job.Job, err error) {
	es, hash, err := script.Embed(s.tb, cl, c, s.api, settings, "", dsd)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	j = s.q.GetOrCreate("embded").Enqueue(ctx, cancel, hash, es, false)
	return
}

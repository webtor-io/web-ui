package job

import (
	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/enrich"
	"github.com/webtor-io/web-ui/services/job"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/web"
)

type Handler struct {
	q        *job.Queues
	tb       template.Builder[*web.Context]
	api      *api.Api
	enricher *enrich.Enricher
}

func New(q *job.Queues, tm *template.Manager[*web.Context], api *api.Api, enricher *enrich.Enricher) *Handler {
	return &Handler{
		q:        q,
		tb:       tm,
		api:      api,
		enricher: enricher,
	}
}

func (s *Handler) RegisterHandler(r *gin.Engine) *Handler {
	r.GET("/queue/:queue_id/job/:job_id/log", s.log)
	return s
}

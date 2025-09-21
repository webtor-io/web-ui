package job

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/services/job"
)

type Handler struct {
	q *job.Queues
}

func RegisterHandler(r *gin.Engine, q *job.Queues) {
	h := &Handler{
		q: q,
	}
	r.GET("/queue/:queue_id/job/:job_id/log", h.log)
}

func (s *Handler) log(c *gin.Context) {
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	l, ok, err := s.q.GetOrCreate(c.Param("queue_id")).Log(ctx, c.Param("job_id"))
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	if !ok {
		c.Status(http.StatusNotFound)
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache,no-store,no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	c.Stream(func(w io.Writer) bool {
		ticker := time.NewTicker(5 * time.Second)
		select {
		case <-ctx.Done():
			ticker.Stop()
			return false
		case <-ticker.C:
			c.SSEvent("ping", "")
			return true
		case msg, ok := <-l:
			if !ok {
				return false
			}
			c.SSEvent("message", msg)
			return true
		}
	})
}

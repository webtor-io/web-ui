package web

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/webtor-io/web-ui/services/metrics"
)

// RecoverToLog is the router's panic recovery. gin.Default's recovery writes
// to stderr only, outside logrus: on 2026-09-18 a rest-api handler panic
// answered ~19k empty 500s a day for 19 hours while the service's own error
// count went down. A panic is logged as an error like any other, with the
// request that triggered it and the stack, so the counters the monitoring
// reads see it.
func RecoverToLog(c *gin.Context, recovered any) {
	log.WithFields(log.Fields{
		"method": c.Request.Method,
		"path":   c.Request.URL.Path,
		"query":  c.Request.URL.RawQuery,
		"panic":  fmt.Sprint(recovered),
		"stack":  string(debug.Stack()),
	}).Error("panic recovered")
	metrics.PanicRecovered(c)
	c.AbortWithStatus(http.StatusInternalServerError)
}

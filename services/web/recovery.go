package web

import (
	"fmt"
	"maps"
	"net/http"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/webtor-io/web-ui/helpers"
	"github.com/webtor-io/web-ui/services/metrics"
)

// RecoverToLog is the router's panic recovery. gin.Default's recovery writes
// to stderr only, outside logrus: on 2026-09-18 a rest-api handler panic
// answered ~19k empty 500s a day for 19 hours while the service's own error
// count went down. A panic is logged as an error like any other, with the
// request that triggered it and the stack, so the counters the monitoring
// reads see it. The path goes through helpers.RedactURL, and of the query
// only the parameter names are kept: a value may be a credential RedactURL
// does not know, such as a Stremio addon URL with its configuration in the
// path.
func RecoverToLog(c *gin.Context, recovered any) {
	log.WithFields(log.Fields{
		"method": c.Request.Method,
		"path":   helpers.RedactURL(c.Request.URL.Path),
		"query":  strings.Join(slices.Sorted(maps.Keys(c.Request.URL.Query())), ","),
		"panic":  fmt.Sprint(recovered),
		"stack":  string(debug.Stack()),
	}).Error("panic recovered")
	metrics.PanicRecovered(c)
	c.AbortWithStatus(http.StatusInternalServerError)
}

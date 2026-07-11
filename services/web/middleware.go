package web

import (
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/webtor-io/web-ui/services/template"
)

// ErrorHandler is the single, centralized error handler. It runs the whole
// request chain via c.Next(), then:
//
//  1. logs every error attached to the context (through c.Error /
//     c.AbortWithError) — so the ORIGINAL error always reaches the logs,
//     regardless of what the user is shown;
//  2. if a middleware or handler aborted WITHOUT writing a response, renders
//     a friendly, localized error page (classified via ClassifyError)
//     instead of leaking a bare 5xx to the user / the Cloudflare error page.
//
// Handlers that render their own error UI, redirect, or write any response
// are left untouched thanks to the c.Writer.Written() guard — this only
// fills the gap where a middleware aborted the chain with just an error.
func ErrorHandler(tb template.Builder[*Context]) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if len(c.Errors) == 0 {
			return
		}

		// (1) always log the real error(s)
		for _, ginErr := range c.Errors {
			log.WithError(ginErr.Err).
				WithField("status", c.Writer.Status()).
				WithField("method", c.Request.Method).
				WithField("path", c.Request.URL.Path).
				Error("request failed")
		}

		// (2) if nothing was written, render the friendly page
		if c.Writer.Written() {
			return
		}

		errKey := ClassifyError(c.Errors.Last().Err)
		status := StatusForErrKey(errKey)
		if wantsJSON(c) {
			c.JSON(status, gin.H{"error": errKey})
			return
		}
		tb.Build("error/page").HTML(status, NewContext(c).WithErrKey(errKey))
	}
}

package web

import (
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

// ErrorLogger is a middleware that logs all errors added to gin.Context
// via c.AbortWithError or c.Error. This allows handlers to simply call
// c.AbortWithError with a wrapped error and skip manual logging.
func ErrorLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		for _, ginErr := range c.Errors {
			log.WithError(ginErr.Err).
				WithField("status", c.Writer.Status()).
				WithField("method", c.Request.Method).
				WithField("path", c.Request.URL.Path).
				Error("request failed")
		}
	}
}

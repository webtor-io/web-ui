package web

import (
	"fmt"
	"io"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/helpers"
)

// AccessLogFormatter is gin's own access line (gin's defaultLogFormatter,
// v1.x), with the path through helpers.RedactURL.
func AccessLogFormatter(param gin.LogFormatterParams) string {
	var statusColor, methodColor, resetColor string
	if param.IsOutputColor() {
		statusColor = param.StatusCodeColor()
		methodColor = param.MethodColor()
		resetColor = param.ResetColor()
	}
	if param.Latency > time.Minute {
		param.Latency = param.Latency.Truncate(time.Second)
	}
	return fmt.Sprintf("[GIN] %v |%s %3d %s| %13v | %15s |%s %-7s %s %#v\n%s",
		param.TimeStamp.Format("2006/01/02 - 15:04:05"),
		statusColor, param.StatusCode, resetColor,
		param.Latency,
		param.ClientIP,
		methodColor, param.Method, resetColor,
		helpers.RedactURL(param.Path),
		param.ErrorMessage,
	)
}

// AccessLog is the access log middleware (serve.go), to gin.DefaultWriter
// as gin.Logger() was.
func AccessLog() gin.HandlerFunc { return AccessLogTo(gin.DefaultWriter) }

// AccessLogTo is AccessLog writing to w.
func AccessLogTo(w io.Writer) gin.HandlerFunc {
	return gin.LoggerWithConfig(gin.LoggerConfig{Formatter: AccessLogFormatter, Output: w})
}

package web

import (
	"fmt"
	"io"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
)

// The access log kept every request's path and query whole, and some carry
// credentials (Loki, 2026-09-30): the Stremio addon's per-user token as a
// path segment (/token/<uuid>/...: whoever has it has the viewer's addon),
// the signed resolve links (/stremio/resolve/<JWT>), a viewer's stream token
// in the query, and the one-time tokens of the unsubscribe and email
// verification links.

// credentialParam is a credential query parameter and its value, raw or
// percent-encoded (a URL passed on inside another).
var credentialParam = regexp.MustCompile(`(?i)((?:^|[?&;]|%3F|%26|%253F|%2526)(?:token|api-key|api_key|apikey)(?:=|%3D|%253D))(?:[A-Za-z0-9._~-]|%2[Ee])+`)

// jwtLike is a JWT wherever it stands.
var jwtLike = regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]*`)

// tokenSegment is the path segment that is a credential: after /token/ (the
// addon), /unsubscribe/ and /email/verify/.
var tokenSegment = regexp.MustCompile(`((?:^|/)(?:token|unsubscribe|verify)/)[^/?#\s]+`)

const redacted = "<redacted>"

// RedactURL is a request path (query included) as the logs may keep it: its
// credentials replaced by "<redacted>", everything else as is.
func RedactURL(s string) string {
	s = credentialParam.ReplaceAllString(s, "${1}"+redacted)
	s = jwtLike.ReplaceAllString(s, redacted)
	return tokenSegment.ReplaceAllString(s, "${1}"+redacted)
}

// AccessLogFormatter is gin's own access line (gin's defaultLogFormatter,
// v1.x), with the path through RedactURL.
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
		RedactURL(param.Path),
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

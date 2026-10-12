package web

import "github.com/gin-gonic/gin"

// FrameAncestors limits which sites may frame this instance's pages:
// Content-Security-Policy: frame-ancestors <sources> on every response except
// the embed entry points, whose job is to be framed by other sites (and,
// where embeds are off, to say so inside the frame). Without it a site that
// lost /embed can frame /ext/magnet?url=… or a resource page instead.
// Empty sources: nil, no header.
func FrameAncestors(sources string) gin.HandlerFunc {
	if sources == "" {
		return nil
	}
	policy := "frame-ancestors " + sources
	return func(c *gin.Context) {
		// The route gin matched, not the request path: gin routes on the raw
		// path, so /%65mbed decodes to "/embed" but is served by the
		// resource catch-all. No route ("") gets the header.
		if !framedByDesign(c.FullPath()) {
			c.Header("Content-Security-Policy", policy)
		}
		c.Next()
	}
}

// framedByDesign: /show is what the SDK puts in its iframe; it redirects to
// /embed.
func framedByDesign(route string) bool {
	return route == "/embed" || route == "/show"
}

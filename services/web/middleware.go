package web

import (
	"context"
	"errors"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/webtor-io/web-ui/services/template"
)

// StatusClientClosedRequest is nginx's 499: the client closed its connection
// before the answer. It is what the log line and webui_http_requests_total
// record for a request the chain gave up on because the client left, so that
// leaving is not counted as a server error (torrent-http-proxy records the
// same code for the same reason). It says the client left, not that nothing
// failed first: see clientGone for the Postgres failure it can hide.
const StatusClientClosedRequest = 499

// clientGone reports whether err reads as the client leaving: the error is
// context.Canceled and the context the server handed the request (client) is
// done — net/http cancels it when the connection closes, HTTP/2 when the
// stream is reset. Both halves matter. A cancel of our own with the client
// still there (a derived context something in the chain cancelled) stays a
// server error; and a failure that reaches this handler as itself — a
// deadline of our own, go-pg's "connection pool timeout", a gRPC error —
// stays that failure even if the client leaves before the answer. The same
// rule as torrent-http-proxy's clientGone.
//
// It cannot see a failure that never reaches it, and go-pg hides one. It
// retries a transient failure — EOF, a reset or refused connection, a
// network timeout (shouldRetry: anything with a Timeout method) — up to
// PG_MAX_RETRIES times (3 in common-services, 100 ms to 1 s apart), sleeping
// between tries on the request's context; when the client leaves during that
// sleep it returns ctx.Err() bare and drops the failure (go-pg v10.15.0
// base.go exec/query, internal.Sleep). "Postgres failed, then the client
// left" therefore arrives here exactly as "the client left while the request
// waited for a pool connection", and is logged and counted as the client's:
// info and 499. A 499 says the client left, not that nothing failed. In the
// PG failover of 2026-09-27 01:19–01:35Z that shape was 45 of the 1324
// "request failed" lines (25 with nothing written, which are 499 now; 20
// answered already, which keep their status and drop to info). The outage
// stays in the error lines through what go-pg does not retry: 308 pool
// timeouts and 646 claims errors in the same window.
//
// Only the context.Canceled sentinel is recognised: go-pg returns it bare
// from a pool wait on a done context and from the retry sleep above (the
// auth middleware's user lookup, services/auth createUser, is the one global
// middleware that aborts with a request-context DB error and no status — the
// shape of the bare "context canceled" with status=200 in the logs),
// net/http's client wraps it. A gRPC status "code = Canceled" or Postgres'
// "canceling statement due to user request" (57014, go-pg cancelling a query
// already running) are not: neither reached this handler on 2026-09-27/28.
func clientGone(client context.Context, err error) bool {
	return client.Err() != nil && errors.Is(err, context.Canceled)
}

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
//
// An error that reads as the client leaving (clientGone) is not treated as a
// server failure — which a Postgres failure go-pg was retrying when the
// client left can be too (clientGone says why it cannot tell the two apart):
// it is logged at info as "client closed request", and when nothing was
// written no page is rendered — nobody is there to read it — and the request
// is recorded as StatusClientClosedRequest (499) instead of the 500 the
// generic page would carry. On 2026-09-27 that was 305 of the 500s on
// /:resource_id/status (and 69 on other pages over 09-27/28), in bursts from
// one viewer closing a page whose dozens of status streams were still
// opening (33 in one second at 18:16:36Z), each one a 5xx the monitoring
// alerts on. gin writes the 499 status line itself once the chain returns;
// it goes to a connection the client has already closed (ingress-nginx drops
// its upstream connection when its client aborts), so no one sees it on the
// wire.
//
// A handler that already wrote its own status (c.AbortWithError writes the
// header at once) keeps it: only the log line changes.
func ErrorHandler(tb template.Builder[*Context]) gin.HandlerFunc {
	return func(c *gin.Context) {
		// The server's context for this request, taken before the chain can
		// replace c.Request with a context of its own (clientGone).
		client := c.Request.Context()

		c.Next()

		if len(c.Errors) == 0 {
			return
		}

		last := c.Errors.Last().Err
		gone := clientGone(client, last)
		if gone && !c.Writer.Written() {
			// Before the log lines below, which carry the status.
			c.Status(StatusClientClosedRequest)
		}

		// (1) always log the real error(s)
		for _, ginErr := range c.Errors {
			l := log.WithError(ginErr.Err).
				WithField("status", c.Writer.Status()).
				WithField("method", c.Request.Method).
				WithField("path", c.Request.URL.Path)
			if clientGone(client, ginErr.Err) {
				l.Info("client closed request")
				continue
			}
			l.Error("request failed")
		}

		// (2) if nothing was written, render the friendly page — unless
		// there is nobody left to show it to
		if c.Writer.Written() || gone {
			return
		}

		errKey := ClassifyError(last)
		status := StatusForErrKey(errKey)
		log.WithError(last).WithField("err_key", errKey).WithField("surface", "page").WithField("path", c.Request.URL.Path).Info("user error shown")
		if wantsJSON(c) {
			c.JSON(status, gin.H{"error": errKey})
			return
		}
		tb.Build("error/page").HTML(status, NewContext(c).WithErrKey(errKey))
	}
}

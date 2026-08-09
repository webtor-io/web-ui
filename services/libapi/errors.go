package libapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

// Error codes clients branch on. They are stable strings, not HTTP statuses:
// a status says "you may not", a code says which of the several reasons applies
// (no key, wrong scope, free plan) — and those need different fixes.
const (
	CodeBadRequest       = "bad_request"
	CodeUnauthorized     = "unauthorized"
	CodeForbidden        = "forbidden"
	CodePaymentRequired  = "payment_required"
	CodeNotFound         = "not_found"
	CodeConflict         = "conflict"
	CodeMethodNotAllowed = "method_not_allowed"
	// CodeRateLimited comes with a Retry-After header; the wait is short
	// (seconds), so the right client reaction is to back off, not to fail over.
	CodeRateLimited = "rate_limited"
	// Device-flow polling codes, named as RFC 8628 §3.5 names them so client
	// libraries written against the RFC branch correctly. All answer 400,
	// like the RFC's token endpoint.
	CodeAuthorizationPending = "authorization_pending"
	CodeSlowDown             = "slow_down"
	CodeExpiredToken         = "expired_token"
	CodeUnavailable          = "unavailable"
	CodeInternal             = "internal_error"
	// CodeUpstream / CodeUpstreamTimeout mark a failure that came from the
	// services behind this one (rest-api, the torrent store, the BitTorrent
	// network). They are kept apart from internal_error because they are often
	// worth retrying, and internal_error is not.
	CodeUpstream        = "upstream_error"
	CodeUpstreamTimeout = "upstream_timeout"
)

// Error is one API error. Status and cause are ours; Code and Message are the
// wire contract.
type Error struct {
	Status  int    `json:"-"`
	Code    string `json:"code" example:"not_found"`
	Message string `json:"message" example:"the specified path does not exist"`
	Err     error  `json:"-"`
}

func (e *Error) Error() string {
	if e.Err != nil {
		return e.Code + ": " + e.Message + ": " + e.Err.Error()
	}
	return e.Code + ": " + e.Message
}

func (e *Error) Unwrap() error {
	return e.Err
}

// ErrorResponse is the body of every failed request. The envelope exists so a
// client can tell an error apart from a successful payload by shape alone,
// whatever the endpoint returns on success.
type ErrorResponse struct {
	Error Error `json:"error"`
}

func NewError(status int, code string, message string, cause error) *Error {
	return &Error{Status: status, Code: code, Message: message, Err: cause}
}

// WriteError renders an error document and aborts the request.
//
// A 404 is how a client asks "does this exist?", so it is logged at debug —
// logging misses at warn level would drown the errors that matter, the same
// reasoning as in services/s3.
func WriteError(c *gin.Context, e *Error) {
	entry := log.WithError(e).WithField("path", c.Request.URL.Path).WithField("method", c.Request.Method)
	switch {
	case e.Status >= http.StatusInternalServerError:
		entry.Error("api request failed")
	case e.Status == http.StatusNotFound:
		entry.Debug("api path not found")
	default:
		entry.Warn("api request rejected")
	}
	c.AbortWithStatusJSON(e.Status, &ErrorResponse{Error: *e})
}

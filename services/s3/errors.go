package s3

import (
	"encoding/xml"
	"net/http"

	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/services/vfs"
)

// S3 error codes we can emit. Clients branch on <Code>, not on the HTTP status,
// so an empty body with the right status is not enough.
const (
	ErrCodeAccessDenied         = "AccessDenied"
	ErrCodeNoSuchBucket         = "NoSuchBucket"
	ErrCodeNoSuchKey            = "NoSuchKey"
	ErrCodeInvalidRequest       = "InvalidRequest"
	ErrCodeInvalidArgument      = "InvalidArgument"
	ErrCodeMethodNotAllowed     = "MethodNotAllowed"
	ErrCodeNotImplemented       = "NotImplemented"
	ErrCodeInternalError        = "InternalError"
	ErrCodeInvalidAccessKey     = "InvalidAccessKeyId"
	ErrCodeSignatureMismatch    = "SignatureDoesNotMatch"
	ErrCodeMissingSecurity      = "MissingSecurityHeader"
	ErrCodeRequestTimeTooSkewed = "RequestTimeTooSkewed"
)

// Error is an S3 protocol error. It carries both the wire code and the HTTP
// status, plus the underlying cause for our own logs (never sent to the client).
type Error struct {
	Code    string
	Status  int
	Message string
	Err     error
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

func newError(status int, code string, message string, cause error) *Error {
	return &Error{Code: code, Status: status, Message: message, Err: cause}
}

// NewError builds an S3 error for callers outside this package (the gin
// handlers, which reject unauthorized requests before the protocol layer runs
// and still owe the client a proper error document).
func NewError(status int, code string, message string, cause error) *Error {
	return newError(status, code, message, cause)
}

// WrongProtocolError reports that a request reached the S3 endpoint speaking
// something else, and returns nil when the method is one S3 uses.
//
// The endpoint is registered for every method (it shares the router helper with
// WebDAV), so a WebDAV client pointed at it lands here and would otherwise be
// told its access key does not exist — which sends people looking for the
// problem in their credentials instead of in their client's protocol.
func WrongProtocolError(method string) *Error {
	switch method {
	case "PROPFIND", "PROPPATCH", "MKCOL", "LOCK", "UNLOCK", "REPORT", "SEARCH":
		return newError(http.StatusMethodNotAllowed, ErrCodeMethodNotAllowed,
			method+" is a WebDAV method and this is the S3 endpoint. "+
				"Use an S3 client here, or the WebDAV URL from your profile for WebDAV.", nil)
	}
	return nil
}

// AnonymousError is what an unsigned request gets. Saying "the access key id
// you provided does not exist" when none was provided is a small lie that costs
// somebody an hour.
func AnonymousError() *Error {
	return newError(http.StatusForbidden, ErrCodeAccessDenied,
		"Anonymous requests are not supported: configure your S3 access key and secret access key", nil)
}

// errorFromVFS maps a filesystem error onto the S3 error the client expects.
// The filesystem speaks HTTP statuses (services/vfs.HTTPError) precisely so both
// protocol layers can do this translation locally.
func errorFromVFS(err error, bucketOnly bool) *Error {
	var s3err *Error
	if errors.As(err, &s3err) {
		return s3err
	}
	he := vfs.HTTPErrorFromError(err)
	switch he.Code {
	case http.StatusNotFound:
		if bucketOnly {
			return newError(http.StatusNotFound, ErrCodeNoSuchBucket, "The specified bucket does not exist", err)
		}
		return newError(http.StatusNotFound, ErrCodeNoSuchKey, "The specified key does not exist", err)
	case http.StatusForbidden:
		return newError(http.StatusForbidden, ErrCodeAccessDenied, "Access Denied", err)
	case http.StatusBadRequest:
		return newError(http.StatusBadRequest, ErrCodeInvalidRequest, "Invalid Request", err)
	case http.StatusMethodNotAllowed:
		return newError(http.StatusMethodNotAllowed, ErrCodeMethodNotAllowed, "The specified method is not allowed against this resource", err)
	default:
		return newError(http.StatusInternalServerError, ErrCodeInternalError, "We encountered an internal error", err)
	}
}

// errorResponse is the S3 error document. Amazon sends it for every failed
// request, including HEAD (where the body is dropped but the status stands).
type errorResponse struct {
	XMLName   xml.Name `xml:"Error"`
	Code      string   `xml:"Code"`
	Message   string   `xml:"Message"`
	Resource  string   `xml:"Resource,omitempty"`
	RequestID string   `xml:"RequestId,omitempty"`
}

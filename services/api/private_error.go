package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/pkg/errors"
	ra "github.com/webtor-io/rest-api/services"
)

// Codes and reasons of rest-api's refusals, as its error body names them.
const (
	CodePrivateTorrent           = "private_torrent"
	CodePrivateTorrentFile       = "private_torrent_file"
	CodePrivateNeedsAccount      = "private_needs_account"
	CodePrivateNoTracker         = "private_no_tracker"
	CodePrivateTooManyTrackers   = "private_too_many_trackers"
	CodePrivateMagnet            = "private_magnet"
	CodePrivateTrackerRefused    = "private_tracker_refused"
	CodePrivateUnavailable       = "private_unavailable"
	CodePrivateVerifyUnavailable = "private_verify_unavailable"
	CodeStoreOutdated            = "store_outdated"
	CodeRateLimited              = "rate_limited" // a 429 that names no code
	ReasonSignIn                 = "sign_in"
	ReasonNoRecord               = "no_record"
)

// PrivateError is rest-api's answer that names a code, or any 429. Callers
// read it by type, never by its text. It carries no URL: a URL may hold a key.
type PrivateError struct {
	Status     int // rest-api's status: 403, 422, 429, 503
	Code       string
	Reason     string // sign_in | no_record | token_in_query, or the tracker's refusal class
	Message    string // rest-api's English text: no URLs, no keys
	RetryAfter string // the raw Retry-After header
}

func (e *PrivateError) Error() string {
	return fmt.Sprintf("private torrent refused: status=%d code=%s reason=%s", e.Status, e.Code, e.Reason)
}

// Is keeps a coded private_magnet the ErrPrivateMagnet it was before rest-api
// named codes: the magnet card, Stremio and the JSON API read it by identity.
func (e *PrivateError) Is(target error) bool {
	return target == ErrPrivateMagnet && e.Code == CodePrivateMagnet
}

func (e *PrivateError) HTTPStatus() int {
	return e.Status
}

// IsPrivateRefusal says rest-api refused a private torrent (403 or 422 with a
// code): a decision about this torrent, not a failure; 503 and 429 are failures.
func IsPrivateRefusal(err error) bool {
	var pe *PrivateError
	return errors.As(err, &pe) && (pe.Status == http.StatusForbidden || pe.Status == http.StatusUnprocessableEntity)
}

// errorFromResponse reads rest-api's answer that is neither 200 nor 404.
// label names the request in errors without a code: the URL, or a constant
// where the path holds what must not reach a log.
func errorFromResponse(res *http.Response, body []byte, label string) error {
	var e ra.ErrorResponse
	perr := json.Unmarshal(body, &e)
	if perr != nil {
		e = ra.ErrorResponse{}
	}
	if e.Code != "" || res.StatusCode == http.StatusTooManyRequests {
		code := e.Code
		if code == "" {
			code = CodeRateLimited
		}
		return &PrivateError{
			Status:     res.StatusCode,
			Code:       code,
			Reason:     e.Reason,
			Message:    e.Error,
			RetryAfter: res.Header.Get("Retry-After"),
		}
	}
	switch res.StatusCode {
	case http.StatusForbidden:
		return fmt.Errorf("%w url=%v", ErrForbidden, label)
	case http.StatusUnprocessableEntity:
		return fmt.Errorf("%w url=%v", ErrPrivateMagnet, label)
	}
	if perr != nil {
		return errors.Wrapf(perr, "failed to parse status=%v body=%v url=%v", res.StatusCode, body, label)
	}
	return errors.New(e.Error)
}

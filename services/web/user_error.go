package web

import (
	"net/http"
	"strings"
)

// UserError wraps an i18n key with the original error for logging.
// Handlers can return UserError to explicitly control the user-facing message.
type UserError struct {
	Key string // i18n key, e.g. "error.forbidden"
	Err error  // original error for logging
}

func (e *UserError) Error() string {
	return e.Key
}

func (e *UserError) Unwrap() error {
	return e.Err
}

// NewUserError creates a UserError with an explicit i18n key.
func NewUserError(key string, err error) *UserError {
	return &UserError{Key: key, Err: err}
}

// ClassifyError determines a user-facing i18n key from the error content.
// If the error is already a UserError, its key is returned directly.
// Otherwise the error message is inspected for known patterns.
func ClassifyError(err error) string {
	if ue, ok := err.(*UserError); ok {
		return ue.Key
	}

	msg := err.Error()

	switch {
	case strings.Contains(msg, "PermissionDenied"),
		strings.Contains(msg, "access is forbidden"),
		strings.Contains(msg, "restricted by the rightholder"):
		return "error.forbidden"

	case strings.Contains(msg, "resource not found"):
		return "error.not_found"

	case strings.Contains(msg, "failed to connect to database"),
		strings.Contains(msg, "failed to get claims"),
		strings.Contains(msg, "failed to create user"),
		strings.Contains(msg, "SuperTokens"),
		strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "Unavailable"):
		// Backend/auth-DB blip: claims-provider, SuperTokens core or the
		// app DB is transiently unreachable. Transient, retry-able.
		return "error.service_unavailable"

	case strings.Contains(msg, "wrong resource provided"),
		strings.Contains(msg, "no resource provided"):
		return "error.invalid_resource"

	case strings.Contains(msg, "failed to load resource"):
		return "error.load_failed"

	case strings.Contains(msg, "over 1080p is not supported"):
		// content-transcoder rejects >1080p non-h264 sources (415 body is
		// embedded into the session-creation error by the api client).
		return "error.resolution_not_supported"

	case strings.Contains(msg, "maximum") && strings.Contains(msg, "allowed"):
		return "error.quota_exceeded"

	case strings.Contains(msg, "already exists"):
		return "error.already_exists"

	case strings.Contains(msg, "frozen"):
		return "error.pledge_frozen"

	case strings.Contains(msg, "unauthorized"):
		return "error.unauthorized"

	case strings.Contains(msg, "access denied"):
		return "error.access_denied"

	case strings.Contains(msg, "failed to validate"):
		return "error.validation_failed"

	// --- streaming chain, in the order the request travels ---

	case strings.Contains(msg, "failed to retrieve resource"),
		strings.Contains(msg, "failed to retrieve stream url"),
		strings.Contains(msg, "failed to retrieve download link"),
		strings.Contains(msg, "stats returned status"),
		strings.Contains(msg, "warmup returned status"):
		// rest-api / torrent-http-proxy / seeder did not answer or answered
		// with an unexpected status. Ours to fix, retry-able; the wording
		// must not blame the torrent.
		return "error.upstream_unavailable"

	case strings.Contains(msg, "failed to get probe data"):
		// content-prober could not read the media: not a video, or a
		// damaged container. Downloading still works.
		return "error.probe_failed"

	case strings.Contains(msg, "transcoder session creation failed status=415"):
		// The transcoder refused the source on purpose (codec, container);
		// >1080p is matched above. The 415 body is in the message for logs.
		return "error.transcode_failed"

	case strings.Contains(msg, "transcoder session creation failed"):
		// Any other status: the converter itself failed to start. Not the
		// file's fault as far as we know — retry-able, download still works.
		return "error.transcode_unavailable"

	case strings.Contains(msg, "session buffer timeout exceeded"),
		strings.Contains(msg, "failed to fetch session master playlist"),
		strings.Contains(msg, "failed to fetch session video playlist"),
		strings.Contains(msg, "failed to parse session video playlist"),
		strings.Contains(msg, "no video variant found"),
		strings.Contains(msg, "too many failed auto-restarts"):
		// The session exists but produced no playable segments in time —
		// the converter was starved by the torrent or died mid-way.
		return "error.stream_stalled"

	default:
		return "error.generic"
	}
}

// StatusForErrKey maps a user-facing error key to the HTTP status the
// centralized ErrorHandler should return. Defaults to 500; the transient
// backend/auth failures map to 503 so clients and Cloudflare treat them as
// retry-able rather than a hard error.
func StatusForErrKey(key string) int {
	switch key {
	case "error.forbidden", "error.access_denied":
		return http.StatusForbidden
	case "error.not_found":
		return http.StatusNotFound
	case "error.unauthorized":
		return http.StatusUnauthorized
	case "error.service_unavailable", "error.upstream_unavailable":
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

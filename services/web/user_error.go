package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/webtor-io/web-ui/services/common"
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
	// --- what was pasted into the form. First, and by identity rather than
	// by text: the query is part of the message ("wrong resource provided
	// query=..."), so a title like "Unavailable" or a magnet named
	// "...Access.Denied..." must not read as a backend outage or a ban ---

	case errors.Is(err, common.ErrMagnetInvalid),
		errors.Is(err, common.ErrMagnetNoHash):
		return "error.magnet_invalid"

	case errors.Is(err, common.ErrV2Only):
		return "error.v2_hash"

	case errors.Is(err, common.ErrQueryWebPage):
		return "error.webpage_url"

	case errors.Is(err, common.ErrQueryTorrentURL):
		return "error.torrent_url"

	case errors.Is(err, common.ErrHashLength):
		return "error.hash_length"

	case errors.Is(err, common.ErrQueryFreeText):
		return "error.free_text"

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

	case strings.Contains(msg, "failed to parse magnet"):
		// The link itself is broken (infohash cut short, wrong encoding):
		// nothing on the network will change that. Above "wrong resource
		// provided", which is how the form wraps it — below it, every broken
		// magnet read "Invalid link or torrent file" instead. The typed
		// ErrMagnetInvalid above covers our own parser; this covers the same
		// text arriving from elsewhere (rest-api, magnet2torrent).
		return "error.magnet_invalid"

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

	// --- magnet resolution: before the chain, it is the step that precedes
	// it. A broken link ("failed to parse magnet") is matched further up ---

	case strings.Contains(msg, "failed to magnetize"),
		strings.Contains(msg, "magnet timeout"):
		// magnet2torrent found no peer with the metadata within the
		// deadline. Measured 2026-09-04: such magnets stay unresolvable on
		// a warm client too (5 of 60), so "try again" is the wrong advice —
		// the message must say nobody is sharing it and point at a .torrent
		// or another source instead of inviting a retry loop.
		return "error.magnet_no_metadata"

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

	case strings.Contains(msg, "transcoder restart limit reached"):
		// The session started, but FFmpeg died on the source six times in
		// a row without a segment and content-transcoder stopped restarting
		// it (a 503 on the playlist, jobs/scripts/hls.go
		// pollSessionPlaylist): a broken subtitle mapping, some AVI and m4b
		// files. A retry dies the same way, so the file's wording — not
		// stream_stalled's "no data from the torrent", and not the no-peers
		// modal the buffer deadline used to end on (~48 a day, 2026-09).
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

// ErrArgs are the numbers a user-facing message quotes, for the one key that
// has any: error.hash_length says how many characters were pasted (Count,
// which also picks the plural form) and how many a full infohash has (Full).
type ErrArgs struct {
	Count int
	Full  int
}

// ErrArgsOf returns the numbers the message for err quotes, or nil when it
// quotes none.
func ErrArgsOf(err error) *ErrArgs {
	var hl *common.HashLengthError
	if errors.As(err, &hl) {
		return &ErrArgs{Count: hl.Len, Full: hl.Full}
	}
	return nil
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
	case "error.magnet_invalid", "error.turnstile_failed",
		"error.free_text", "error.webpage_url", "error.torrent_url", "error.v2_hash",
		"error.hash_length":
		return http.StatusBadRequest
	case "error.magnet_no_metadata":
		// Nothing answered upstream within the deadline: a gateway timeout,
		// not a server fault.
		return http.StatusGatewayTimeout
	default:
		return http.StatusInternalServerError
	}
}

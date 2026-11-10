package helpers

import "regexp"

// The access log kept every request's path and query whole, and some carry
// credentials (Loki, 2026-09-30): the Stremio addon's per-user token as a
// path segment (/token/<uuid>/...: whoever has it has the viewer's addon),
// the signed resolve links (/stremio/resolve/<JWT>), a viewer's stream token
// in the query, and the one-time tokens of the unsubscribe and email
// verification links. A magnet's trackers are one more: a private tracker's
// passkey is part of its tracker URL.

// credentialParam is a credential query parameter and its value, raw or
// percent-encoded (a URL passed on inside another). passkey and pk are a
// private tracker's, standing on their own when a magnet carries a tracker
// URL unencoded: its "&passkey=" becomes a parameter of the magnet.
var credentialParam = regexp.MustCompile(`(?i)((?:^|[?&;]|%3F|%26|%253F|%2526)(?:token|api-key|api_key|apikey|passkey|pk)(?:=|%3D|%253D))(?:[A-Za-z0-9._~-]|%2[Ee])+`)

// trackerParam is a magnet's tr= value read as is (a magnet page's own
// query, a magnet in HTML, where "&" is "&amp;"): the whole tracker URL, up
// to the next "&".
var trackerParam = regexp.MustCompile(`(?i)((?:^|[?&;])tr=)[^&#\s"'<>]+`)

// trackerParamEncoded is the same value in a magnet that is itself a query
// value (/ext/magnet?url=magnet%3A%3F…%26tr%3D…): there the magnet's
// parameters are separated by %26, so the value runs to the next %26 or
// "&". Twice encoded, its separator is %2526 and the value runs on to the
// end of the outer parameter: more is hidden, never less.
var trackerParamEncoded = regexp.MustCompile(`(?i)((?:%3F|%26|%253F|%2526)tr(?:%3D|%253D))(?:[^&%#\s"'<>]|%[013-9A-F][0-9A-F]|%2[0-57-9A-F])+`)

// jwtLike is a JWT wherever it stands.
var jwtLike = regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]*`)

// tokenSegment is the path segment that is a credential: after /token/ (the
// addon), /unsubscribe/ and /email/verify/.
var tokenSegment = regexp.MustCompile(`((?:^|/)(?:token|unsubscribe|verify)/)[^/?#\s]+`)

const redacted = "<redacted>"

// RedactURL is a request path (query included), a whole URL, or any log text
// carrying one, as the logs may keep it: its credentials replaced by
// "<redacted>", everything else as is.
func RedactURL(s string) string {
	s = trackerParam.ReplaceAllString(s, "${1}"+redacted)
	s = trackerParamEncoded.ReplaceAllString(s, "${1}"+redacted)
	s = credentialParam.ReplaceAllString(s, "${1}"+redacted)
	s = jwtLike.ReplaceAllString(s, redacted)
	return tokenSegment.ReplaceAllString(s, "${1}"+redacted)
}

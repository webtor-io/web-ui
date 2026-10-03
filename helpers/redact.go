package helpers

import "regexp"

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

// RedactURL is a request path (query included), a whole URL, or any log text
// carrying one, as the logs may keep it: its credentials replaced by
// "<redacted>", everything else as is.
func RedactURL(s string) string {
	s = credentialParam.ReplaceAllString(s, "${1}"+redacted)
	s = jwtLike.ReplaceAllString(s, redacted)
	return tokenSegment.ReplaceAllString(s, "${1}"+redacted)
}

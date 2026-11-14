package helpers

import (
	"regexp"
	"strings"
)

// The access log kept every request's path and query whole, and some carry
// credentials (Loki, 2026-09-30): the Stremio addon's per-user token as a
// path segment (/token/<uuid>/...: whoever has it has the viewer's addon),
// the signed resolve links (/stremio/resolve/<JWT>), a viewer's stream token
// in the query, and the one-time tokens of the unsubscribe and email
// verification links. A magnet is one more: a private tracker's passkey is
// part of its tracker URL (tr=), and the user's key of its download link
// (as=, xs=).

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

// magnet is a magnet link and its parameters: as is, percent-encoded once
// (/ext/magnet?url=magnet%3A%3F…) or twice (that address passed on as a
// parameter of another). As is, "?" may be missing (the /magnet: route's
// query is path + RawQuery), and only a quote or a tag ends it: a query
// pasted with a line break or a space is still one magnet, and a magnet
// page's address runs to its end. Encoded, it ends where the URL it is a
// value of goes on ("&", "#", whitespace); twice encoded, also at "%26",
// which redactMagnet cuts (RE2 has no lookahead).
var magnet = regexp.MustCompile(`(?i)magnet:(?:\?|[a-z][a-z0-9.]*=)[^"<>]*|magnet%3A%3F[^&#\s"<>]*|magnet%253A%253F[^&#\s"<>]*`)

// magnetKept are the parameters a magnet keeps in the logs: the infohash,
// the name and the size. Any other one can carry a private tracker's
// credential or a link that fetches it -- tr=, as=, xs=, ws=, x.pe=, and the
// keys an unencoded tracker URL spills into the magnet (uk=, authkey=) -- so
// it is an allowlist: a name nobody thought of is hidden too.
var magnetKept = map[string]bool{"xt": true, "dn": true, "xl": true}

// magnetEq is a magnet parameter's "=", at any of the three levels.
var magnetEq = regexp.MustCompile(`(?i)=|%3D|%253D`)

// redactMagnet is m, a match of magnet, with the value of every parameter
// but magnetKept's replaced.
func redactMagnet(m string) string {
	n, rest := len("magnet:"), ""
	switch {
	case hasPrefixFold(m, "magnet%253A%253F"):
		n = len("magnet%253A%253F")
		if i := strings.Index(m, "%26"); i >= 0 {
			m, rest = m[:i], m[i:]
		}
	case hasPrefixFold(m, "magnet%3A%3F"):
		n = len("magnet%3A%3F")
	case hasPrefixFold(m, "magnet:?"):
		n = len("magnet:?")
	}
	// Every level's separator, whatever the magnet's own: a magnet can be
	// encoded in part ("magnet:?xt%3D…%26tr%3D…", which
	// common.ResolveQueryHash takes). A split inside a value hides more of
	// it; only a parameter of a tracker URL's own query that is named xt, dn
	// or xl would stay.
	return m[:n] + redactMagnetParams(m[n:], "&", "%26", "%2526") + rest
}

// redactMagnetParams is s split at seps[0], each part split further at the
// rest and redacted (redactMagnetParam).
func redactMagnetParams(s string, seps ...string) string {
	if len(seps) == 0 {
		return redactMagnetParam(s)
	}
	parts := strings.Split(s, seps[0])
	for i, p := range parts {
		parts[i] = redactMagnetParams(p, seps[1:]...)
	}
	return strings.Join(parts, seps[0])
}

func redactMagnetParam(p string) string {
	// In HTML the separator is "&amp;": the split leaves "amp;" in front.
	pre := ""
	if hasPrefixFold(p, "amp;") {
		pre, p = p[:4], p[4:]
	}
	eq := magnetEq.FindStringIndex(p)
	switch {
	case p == "":
		return pre
	case eq == nil:
		return pre + redacted
	case magnetKept[strings.ToLower(p[:eq[0]])], eq[1] == len(p):
		// An empty value stays empty: a second pass over a line already
		// redacted sees "tr=" followed by "<redacted>".
		return pre + p
	}
	return pre + p[:eq[1]] + redacted
}

func hasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

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
	s = magnet.ReplaceAllStringFunc(s, redactMagnet)
	s = trackerParam.ReplaceAllString(s, "${1}"+redacted)
	s = trackerParamEncoded.ReplaceAllString(s, "${1}"+redacted)
	s = credentialParam.ReplaceAllString(s, "${1}"+redacted)
	s = jwtLike.ReplaceAllString(s, redacted)
	return tokenSegment.ReplaceAllString(s, "${1}"+redacted)
}

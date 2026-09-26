package api

import (
	"github.com/golang-jwt/jwt/v5"
	"github.com/pkg/errors"
)

// Rule is shared with THP. Web-ui sets Rules on its outgoing Claims before
// calling rest-api; rest-api copies the X-Token header into the ?token= of
// every signed export URL, so rules reach THP without any re-signing here.
type Rule struct {
	Kind        string `json:"kind"`
	Scope       string `json:"scope"`
	DurationSec int    `json:"duration_sec"`
	Token       string `json:"token"`
}

// GraceClaims is the standalone JWT swapped in by THP on segment URLs while
// movie-time falls inside the grace window. Hash binding prevents replay on
// other torrents. The window itself is movie time, not wall time; the token
// expires with the primary token that carries it (NewGraceClaims), since it
// names the viewer's session: without an expiry a segment URL copied out of
// a playlist charged that session's grace bucket, per-session caps and
// /session-stats for as long as anyone used it.
//
// SessionID and Domain are the primary token's, named as Claims names them
// (NewGraceClaims copies them). With them thp limits grace segments at the
// grace rate per session — a bucket per (sessionID, rate), apart from the
// tier's — and counts their bytes, open requests and presence in the
// viewer's /session-stats, under the same (sessionID, domain, infohash) key
// as the rest of the viewer's requests, but not in its `rate` or `throttled`
// (grace is not the tier binding). Without them grace segments were neither
// limited nor counted: the transfer status was blind inside the window.
// Needs a thp that keys its limiter by (session, rate) and knows grace
// tokens (`kind`) in its stats: docs/grace_token.md "Session".
type GraceClaims struct {
	Rate      string `json:"rate"`
	Role      string `json:"role"`
	Hash      string `json:"hash"`
	Kind      string `json:"kind"`
	SessionID string `json:"sessionID"`
	Domain    string `json:"domain"`
	jwt.RegisteredClaims
}

// NewGraceClaims is the grace token for the viewer of primary on the torrent
// hash, at rate: bound to the torrent, and to the primary token's session,
// domain and expiry (GraceClaims). thp hands it out only while it serves a
// playlist on a valid primary, so no playback needs it past the primary's
// expiry. nil primary — no session to carry, no expiry.
func NewGraceClaims(primary *Claims, hash, rate string) GraceClaims {
	g := GraceClaims{Rate: rate, Role: "grace", Hash: hash, Kind: "grace"}
	if primary != nil {
		g.SessionID, g.Domain = primary.SessionID, primary.Domain
		g.ExpiresAt = primary.ExpiresAt
	}
	return g
}

// SignClaims signs an arbitrary jwt.Claims payload with the API HS256 secret.
// Used to mint the inner grace token; the outer primary Claims (carrying the
// grace token in Rules) is signed by the existing prepareRequest path.
func (s *Api) SignClaims(c jwt.Claims) (string, error) {
	if s.secret == "" {
		return "", errors.New("api secret not configured")
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(s.secret))
}

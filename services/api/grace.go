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
// other torrents. No expiry — enforcement is movie-time-bound, not walltime.
type GraceClaims struct {
	Rate string `json:"rate"`
	Role string `json:"role"`
	Hash string `json:"hash"`
	Kind string `json:"kind"`
	jwt.RegisteredClaims
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

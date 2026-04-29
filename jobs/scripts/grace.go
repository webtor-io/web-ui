package scripts

import (
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/web"
)

// GraceSettings groups grace-rule tuning knobs so call-sites pass a single
// value rather than three loose flags. Wired from CLI/env in jobs.New() and
// threaded through Action script unchanged.
type GraceSettings struct {
	// Enabled gates the whole feature. When false, no grace rules are issued
	// regardless of tier.
	Enabled bool
	// DurationSec is the movie-time window during which THP serves the grace
	// rate. Surfaced to the player JS so it knows when to show the soft
	// signup CTA after the grace window passes.
	DurationSec int
	// Rate is the privileged rate string (e.g. "50M") embedded in the inner
	// grace JWT. THP swaps to this rate while inside the window.
	Rate string
}

// isFreeTier returns true for both anonymous and explicitly-free authenticated
// users. ApiClaims.Role mirrors claims-provider tier (or empty for anon), so
// this check is the single source of truth for "should we issue grace?".
func isFreeTier(c *web.Context) bool {
	if c == nil || c.ApiClaims == nil {
		return true
	}
	role := c.ApiClaims.Role
	return role == "" || role == "free" || role == "nobody"
}

// applyGraceRules signs a hash-bound grace token and attaches it as a manifest
// rule on the outgoing primary claims. rest-api copies the X-Token header
// verbatim into the ?token= query of every signed export URL — so rules reach
// THP without any URL re-signing on our side. Errors are logged but
// non-fatal: the user just falls back to the regular plan rate.
//
// Must be called BEFORE Api.ExportResourceContent so the rules ride along
// inside that request's X-Token header.
func (s *ActionScript) applyGraceRules(sc *StreamContent, hash string, c *web.Context) {
	graceTok, err := s.api.SignClaims(api.GraceClaims{
		Rate: s.grace.Rate,
		Role: "grace",
		Hash: hash,
		Kind: "grace",
	})
	if err != nil {
		log.WithError(err).Warn("failed to sign grace token")
		return
	}
	c.ApiClaims.Rules = []api.Rule{{
		Kind:        "grace",
		Scope:       "manifest",
		DurationSec: s.grace.DurationSec,
		Token:       graceTok,
	}}
	// Bind the primary token to this infohash too. THP's generic
	// claims["hash"] != src.InfoHash → 403 check then fires on the
	// manifest request itself, not just on the inner grace token at
	// segment time. Closes the "PRIMARY of A reused on torrent B"
	// replay path with no THP changes.
	c.ApiClaims.Hash = hash
	sc.GraceDurationSec = s.grace.DurationSec
	if bps := parseRateLimit(c.ApiClaims.Rate); bps > 0 {
		sc.GraceFreeRateMbps = int(bps / 1_000_000)
	}
}

package resource

import (
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pkg/errors"
)

// Audiences keep a token minted for one purpose from being accepted for
// another, even though all of them sign with the session secret.
const (
	torrentFileAudience = "torrent-file"
	statusAudience      = "torrent-status"
)

// torrentFileTokenTTL is how long a .torrent link rendered on a resource page
// keeps working. Long enough for a tab left open over an evening, short
// enough that a link copied onto a torrent index is dead by the time its
// crawlers come back: in September 2026 four .torrent files carrying
// single-file malware payloads were fetched 1.4 million times a week through
// links planted on itorrents/limetorrents, with webtor.io as the host.
const torrentFileTokenTTL = 6 * time.Hour

// statusTokenTTL is how long the status stream stays openable from one
// page render. The stream itself lives minutes and the client reopens it
// with the same token, so this bounds how long a tab keeps a live badge
// without a reload. Bots that cannot load the page (it is challenged at the
// edge) cannot mint one; a harvested pair of session cookie and CSRF token
// used to open the stream indefinitely — 2026-09-07, ~1 000 streams per
// half hour under forged Referer headers, each one loading a torrent on a
// seeder.
const statusTokenTTL = time.Hour

func signResourceToken(secret, audience, infohash string, ttl time.Duration, now time.Time) (string, error) {
	if secret == "" {
		return "", errors.Errorf("cannot sign a %s token without a secret", audience)
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": strings.ToLower(infohash),
		"aud": audience,
		"iat": now.Unix(),
		"exp": now.Add(ttl).Unix(),
	})
	return token.SignedString([]byte(secret))
}

func checkResourceToken(secret, audience, raw, infohash string, now time.Time) error {
	if secret == "" {
		return errors.Errorf("cannot verify a %s token without a secret", audience)
	}
	_, err := jwt.Parse(strings.TrimSpace(raw), func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	}, jwt.WithAudience(audience), jwt.WithSubject(strings.ToLower(infohash)), jwt.WithExpirationRequired(), jwt.WithTimeFunc(func() time.Time { return now }))
	if err != nil {
		return errors.Wrapf(err, "invalid %s token", audience)
	}
	return nil
}

// SignTorrentFileToken mints the credential that rides in a .torrent link.
// It is bound to the infohash, so a token lifted from one page cannot fetch
// another torrent.
func SignTorrentFileToken(secret, infohash string, now time.Time) (string, error) {
	return signResourceToken(secret, torrentFileAudience, infohash, torrentFileTokenTTL, now)
}

// CheckTorrentFileToken validates a token against the infohash being fetched.
func CheckTorrentFileToken(secret, raw, infohash string, now time.Time) error {
	return checkResourceToken(secret, torrentFileAudience, raw, infohash, now)
}

// SignStatusToken mints the credential the page hands to the status stream.
func SignStatusToken(secret, infohash string, now time.Time) (string, error) {
	return signResourceToken(secret, statusAudience, infohash, statusTokenTTL, now)
}

// CheckStatusToken validates a status-stream token for the resource.
func CheckStatusToken(secret, raw, infohash string, now time.Time) error {
	return checkResourceToken(secret, statusAudience, raw, infohash, now)
}

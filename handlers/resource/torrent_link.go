package resource

import (
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pkg/errors"
)

// torrentFileAudience keeps a token minted for a .torrent download from being
// accepted anywhere else that signs with the session secret, and vice versa.
const torrentFileAudience = "torrent-file"

// torrentFileTokenTTL is how long a .torrent link rendered on a resource page
// keeps working. Long enough for a tab left open over an evening, short
// enough that a link copied onto a torrent index is dead by the time its
// crawlers come back: in September 2026 four .torrent files carrying
// single-file malware payloads were fetched 1.4 million times a week through
// links planted on itorrents/limetorrents, with webtor.io as the host.
const torrentFileTokenTTL = 6 * time.Hour

// SignTorrentFileToken mints the credential that rides in a .torrent link.
// It is bound to the infohash, so a token lifted from one page cannot fetch
// another torrent.
func SignTorrentFileToken(secret, infohash string, now time.Time) (string, error) {
	if secret == "" {
		return "", errors.New("cannot sign a torrent-file token without a secret")
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": strings.ToLower(infohash),
		"aud": torrentFileAudience,
		"iat": now.Unix(),
		"exp": now.Add(torrentFileTokenTTL).Unix(),
	})
	return token.SignedString([]byte(secret))
}

// CheckTorrentFileToken validates a token against the infohash being fetched.
func CheckTorrentFileToken(secret, raw, infohash string, now time.Time) error {
	if secret == "" {
		return errors.New("cannot verify a torrent-file token without a secret")
	}
	_, err := jwt.Parse(strings.TrimSpace(raw), func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	}, jwt.WithAudience(torrentFileAudience), jwt.WithSubject(strings.ToLower(infohash)), jwt.WithExpirationRequired(), jwt.WithTimeFunc(func() time.Time { return now }))
	if err != nil {
		return errors.Wrap(err, "invalid torrent-file token")
	}
	return nil
}

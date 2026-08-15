package release_subscription

import (
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
)

// unsubscribeAudience keeps a token minted here from being accepted anywhere
// else that signs with the session secret (playback tokens, the Stremio
// resolve URLs), and vice versa.
const unsubscribeAudience = "release-subscription-unsubscribe"

// SignUnsubscribeToken mints the one-click unsubscribe credential that rides
// in every subscription email.
//
// It carries no expiry. The link has to work on an email read six months
// late, and it cannot outlive its subject anyway: unsubscribing deletes the
// row, after which the token resolves to nothing.
func SignUnsubscribeToken(secret string, id uuid.UUID) (string, error) {
	if secret == "" {
		return "", errors.New("cannot sign an unsubscribe token without a secret")
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": id.String(),
		"aud": unsubscribeAudience,
	})
	return token.SignedString([]byte(secret))
}

// ParseUnsubscribeToken validates a token and returns the subscription it
// addresses.
func ParseUnsubscribeToken(secret, raw string) (uuid.UUID, error) {
	if secret == "" {
		return uuid.Nil, errors.New("cannot verify an unsubscribe token without a secret")
	}
	token, err := jwt.Parse(strings.TrimSpace(raw), func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	}, jwt.WithAudience(unsubscribeAudience))
	if err != nil {
		return uuid.Nil, errors.Wrap(err, "invalid unsubscribe token")
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return uuid.Nil, errors.New("invalid unsubscribe token claims")
	}
	sub, _ := claims["sub"].(string)
	id, err := uuid.FromString(sub)
	if err != nil {
		return uuid.Nil, errors.Wrap(err, "invalid subscription id in unsubscribe token")
	}
	return id, nil
}

// UnsubscribeURL builds the link an email renders. Returns "" when the token
// cannot be signed — a letter without an unsubscribe link is bad, but a
// letter that fails to send is worse, so the caller decides.
func UnsubscribeURL(domain, secret string, id uuid.UUID) string {
	token, err := SignUnsubscribeToken(secret, id)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%s/subscription/unsubscribe/%s", strings.TrimRight(domain, "/"), token)
}

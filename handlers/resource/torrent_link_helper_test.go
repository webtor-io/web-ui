package resource

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func mintForAudience(t *testing.T, secret, sub, aud string, now time.Time) string {
	t.Helper()
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": sub, "aud": aud, "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	}).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

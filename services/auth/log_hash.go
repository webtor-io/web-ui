package auth

import (
	"crypto/md5"
	"encoding/hex"

	uuid "github.com/satori/go.uuid"
)

// LogHash names an account in log lines that are read in aggregate — funnel
// counts like "saw the Stremio paywall, then got a plan" — rather than to
// debug one request. It is a stable pseudonym: the same account always
// gives the same value, whoever has only the logs cannot turn it back into
// an account, and whoever has the database can join on it in SQL:
//
//	left(md5(user_id::text), 16)
//
// md5 of the canonical lowercase UUID text is chosen for that SQL form (the
// winback holdout buckets on the same md5(user_id::text)); it is not a
// security boundary. "" for an anonymous request, so a missing account is
// not logged as the hash of the nil UUID.
func LogHash(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	sum := md5.Sum([]byte(id.String()))
	return hex.EncodeToString(sum[:])[:16]
}

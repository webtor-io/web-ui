// Package adminauth owns the single-administrator password used when the
// instance runs without SuperTokens (self-hosted). It is deliberately
// separate from services/auth: that package speaks SuperTokens sessions,
// this one only answers "is this the right password".
package adminauth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// MinLength is a floor on length only. Composition rules (a digit, a symbol)
// push people towards Password1! rather than towards long passphrases, so we
// do not impose them.
const MinLength = 8

var ErrTooShort = errors.New("password must be at least 8 characters")

// argon2id parameters, OWASP's second recommended profile: 64 MiB of memory,
// 3 passes, 4 lanes. They are encoded into every hash, so raising them later
// only affects newly written hashes — old ones keep verifying with the
// parameters they were made with.
const (
	hashMemory  uint32 = 64 * 1024
	hashTime    uint32 = 3
	hashThreads uint8  = 4
	hashKeyLen  uint32 = 32
	hashSaltLen        = 16
)

// Bounds Verify enforces on the parameters it reads out of an encoded hash,
// before ever calling argon2.IDKey. Those parameters come from the
// database, not from this package, so they must be treated as untrusted:
// argon2.IDKey panics outright for time=0 or threads=0, and otherwise has
// no upper limit at all — a large enough m tries to allocate memory
// proportional to it (a hash claiming m=4_000_000_000, i.e. ~3.7 TiB, drove
// RSS past 4 GiB in three seconds in testing before being killed).
//
// The bounds below are chosen to comfortably admit what Hash writes today
// (m=64Mi KiB, t=3, p=4) plus a generous margin for raising those constants
// later, while rejecting anything a corrupt or hostile row could use to
// panic or exhaust memory:
//   - maxHashMemory is 1 GiB (in KiB) — 16x today's 64 MiB. That is still a
//     small, safe allocation on any host capable of running this service,
//     and it is orders of magnitude below the multi-terabyte requests the
//     lack of a bound previously allowed through.
//   - maxHashTime is 32 — about 10x today's t=3. Time is a linear
//     multiplier on the cost of a single verify; this stays well within a
//     login-path latency budget even at the ceiling.
//   - maxHashThreads is 32 — 8x today's p=4, comfortably above any
//     realistic core count for a box running this service.
const (
	maxHashMemory  uint32 = 1 << 20 // KiB, i.e. 1 GiB
	maxHashTime    uint32 = 32
	maxHashThreads uint8  = 32
)

// Hash returns an encoded argon2id hash of password.
func Hash(password string) (string, error) {
	if len([]rune(password)) < MinLength {
		return "", ErrTooShort
	}
	salt := make([]byte, hashSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("failed to generate salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, hashTime, hashMemory, hashThreads, hashKeyLen)
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, hashMemory, hashTime, hashThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// Verify reports whether password matches the encoded hash. Any problem with
// the encoding — empty, truncated, produced by another algorithm — is a
// mismatch, never a panic: the value arrives from the database.
func Verify(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}
	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false
	}
	// argon2.IDKey panics for time=0 or threads=0, and has no upper bound of
	// its own on any parameter — reject implausible values here, before the
	// call, rather than relying on it to survive them. See maxHash* above.
	if memory < 1 || memory > maxHashMemory {
		return false
	}
	if time < 1 || time > maxHashTime {
		return false
	}
	if threads < 1 || threads > maxHashThreads {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

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

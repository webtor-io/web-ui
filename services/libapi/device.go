package libapi

import (
	"crypto/rand"
	"strings"
	"time"
)

// Device authorization (RFC 8628 shaped): a device asks for a code pair,
// a person confirms the short user code in a browser session on the site,
// the device's next poll receives a per-device API key.
const (
	// DevicePath is where the confirmation page lives on the main domain.
	DevicePath = "/device"

	// DeviceCodeTTLSeconds is how long a person has to confirm. Ten minutes
	// is the RFC's own example; shorter frustrates TV keyboards, longer
	// stretches the brute-force window for no benefit.
	DeviceCodeTTLSeconds = 600

	// DevicePollIntervalSeconds is the slowest poll cadence a device may use.
	DevicePollIntervalSeconds = 5

	// DevicePollRPS is the token-bucket rate matching DevicePollIntervalSeconds.
	DevicePollRPS = 1.0 / DevicePollIntervalSeconds

	// DeviceTokenPrefix names the issued per-device access_token rows.
	// A distinct prefix is what lets the profile list and revoke devices
	// without touching the account's own "api" key.
	DeviceTokenPrefix = "device:"
)

// DeviceCodeTTL is DeviceCodeTTLSeconds as a duration.
const DeviceCodeTTL = DeviceCodeTTLSeconds * time.Second

// userCodeAlphabet omits everything a person squinting at a TV can confuse:
// no 0/O, no 1/I/L. 28 symbols, 8 positions ≈ 38 bits — plenty against a
// 10-minute window behind a rate limiter.
const userCodeAlphabet = "23456789BCDFGHJKMNPQRSTVWXYZ"

// NewUserCode returns a fresh "XXXX-XXXX" code. Uniqueness is the caller's
// problem (retry on the DB's unique violation) — the code space is small on
// purpose, because a person types it.
func NewUserCode() (string, error) {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	var b strings.Builder
	for i, r := range raw {
		if i == 4 {
			b.WriteByte('-')
		}
		b.WriteByte(userCodeAlphabet[int(r)%len(userCodeAlphabet)])
	}
	return b.String(), nil
}

// NormalizeUserCode maps what a person actually typed onto the canonical
// form: case-insensitive, dash optional, surrounding junk ignored.
func NormalizeUserCode(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, " ", "")
	if len(s) != 8 {
		return ""
	}
	return s[:4] + "-" + s[4:]
}

// SanitizeDeviceName bounds the caller-supplied label that ends up in the
// profile's device list. It is display text from an unauthenticated caller —
// keep it short and printable.
func SanitizeDeviceName(s string) string {
	s = strings.TrimSpace(s)
	out := make([]rune, 0, 64)
	for _, r := range s {
		if r < 32 || r == 127 {
			continue
		}
		out = append(out, r)
		if len(out) == 64 {
			break
		}
	}
	return string(out)
}

// DeviceCodeResponse is the reply to a device's initial request, RFC 8628 §3.2
// field names so existing client libraries parse it unchanged.
type DeviceCodeResponse struct {
	DeviceCode              string `json:"device_code" example:"6c0b8bad-4b41-4bcb-9d10-4c0a0a8e1e3f"`
	UserCode                string `json:"user_code" example:"F7KQ-29XD"`
	VerificationURI         string `json:"verification_uri" example:"https://webtor.io/device"`
	VerificationURIComplete string `json:"verification_uri_complete" example:"https://webtor.io/device?code=F7KQ-29XD"`
	ExpiresIn               int    `json:"expires_in" example:"600"`
	Interval                int    `json:"interval" example:"5"`
}

// DeviceCodeRequest is the optional body of the initial request.
type DeviceCodeRequest struct {
	// Name labels the issued key in the profile's device list,
	// e.g. "webtor-cli @ macbook". Optional.
	Name string `json:"name,omitempty" example:"webtor-cli @ macbook"`
}

// DeviceTokenRequest is the poll body.
type DeviceTokenRequest struct {
	DeviceCode string `json:"device_code" binding:"required" example:"6c0b8bad-4b41-4bcb-9d10-4c0a0a8e1e3f"`
}

// DeviceTokenResponse delivers the issued key — exactly once.
type DeviceTokenResponse struct {
	// Key is a per-device API key; send it as `Authorization: Bearer <key>`.
	// Revocable on the profile page without touching other devices.
	Key string `json:"key" example:"99999999-8888-7777-6666-555555555555"`
}

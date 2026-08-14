package torznab

import (
	"regexp"
)

// secretParams matches the credential-carrying query parameters an indexer
// URL can hold. Torznab passes the key in the URL, so every error a failed
// request produces embeds it — go-jackett's errors quote the full URL, and
// those errors reach both logrus (and therefore Loki) and, on the refresh
// endpoint, the browser.
var secretParams = regexp.MustCompile(`(?i)\b(apikey|api_key|jackett_apikey|passkey|token|rss_?key)=([^&\s"'\\]+)`)

// Redact replaces credential values in a string with ***.
func Redact(s string) string {
	return secretParams.ReplaceAllString(s, "$1=***")
}

// redactedError keeps the original error reachable for errors.Is/As while
// making its message safe to log. Wrapping with %w and a redacted prefix
// would not be enough: the message of the wrapped error is what leaks.
type redactedError struct {
	err error
	msg string
}

func (e *redactedError) Error() string { return e.msg }

func (e *redactedError) Unwrap() error { return e.err }

// RedactError returns err with credentials stripped from its message. The
// original error stays wrapped, so IsUnreachable and errors.As keep working.
func RedactError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	redacted := Redact(msg)
	if redacted == msg {
		return err
	}
	return &redactedError{err: err, msg: redacted}
}

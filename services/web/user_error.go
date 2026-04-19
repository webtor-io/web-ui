package web

import (
	"strings"
)

// UserError wraps an i18n key with the original error for logging.
// Handlers can return UserError to explicitly control the user-facing message.
type UserError struct {
	Key string // i18n key, e.g. "error.forbidden"
	Err error  // original error for logging
}

func (e *UserError) Error() string {
	return e.Key
}

func (e *UserError) Unwrap() error {
	return e.Err
}

// NewUserError creates a UserError with an explicit i18n key.
func NewUserError(key string, err error) *UserError {
	return &UserError{Key: key, Err: err}
}

// ClassifyError determines a user-facing i18n key from the error content.
// If the error is already a UserError, its key is returned directly.
// Otherwise the error message is inspected for known patterns.
func ClassifyError(err error) string {
	if ue, ok := err.(*UserError); ok {
		return ue.Key
	}

	msg := err.Error()

	switch {
	case strings.Contains(msg, "PermissionDenied"),
		strings.Contains(msg, "access is forbidden"),
		strings.Contains(msg, "restricted by the rightholder"):
		return "error.forbidden"

	case strings.Contains(msg, "resource not found"):
		return "error.not_found"

	case strings.Contains(msg, "failed to connect to database"):
		return "error.service_unavailable"

	case strings.Contains(msg, "wrong resource provided"),
		strings.Contains(msg, "no resource provided"):
		return "error.invalid_resource"

	case strings.Contains(msg, "failed to load resource"):
		return "error.load_failed"

	case strings.Contains(msg, "maximum") && strings.Contains(msg, "allowed"):
		return "error.quota_exceeded"

	case strings.Contains(msg, "already exists"):
		return "error.already_exists"

	case strings.Contains(msg, "frozen"):
		return "error.pledge_frozen"

	case strings.Contains(msg, "unauthorized"):
		return "error.unauthorized"

	case strings.Contains(msg, "access denied"):
		return "error.access_denied"

	case strings.Contains(msg, "failed to validate"):
		return "error.validation_failed"

	default:
		return "error.generic"
	}
}

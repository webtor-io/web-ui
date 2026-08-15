package models

import (
	"context"
	"errors"
	"time"

	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
)

// UserSettings carries per-user preferences that don't fit existing
// tables. Single row per user. Missing row = everything at default —
// callers must treat the nil return from GetUserSettings as "use the
// type's zero values" rather than a fault.
type UserSettings struct {
	tableName struct{} `pg:"user_settings"`

	UserID uuid.UUID `pg:"user_id,pk"`
	// ShowAdult: when true the unified poster endpoint serves the
	// /raw/ variant (no Gaussian blur) and the 18+ overlay-badge
	// isn't rendered on cards. Default false — accidental-view
	// protection is the safer baseline.
	ShowAdult bool `pg:"show_adult,use_zero,notnull"`
	// Lang is the interface language last seen on an authenticated request.
	// It exists because notifications are sent from a cron job, where the
	// URL prefix and the lang cookie that carry the language everywhere else
	// are both out of reach. Nil means "never observed" — a different thing
	// from "chose English", so senders fall back rather than assume.
	Lang      *string   `pg:"lang"`
	CreatedAt time.Time `pg:"created_at,notnull"`
	UpdatedAt time.Time `pg:"updated_at,notnull"`
}

// GetUserSettings returns the row for a user, or nil when none exists.
// Callers should treat nil as "user is on defaults" (ShowAdult=false,
// future fields likewise at zero). Anonymous flows that have no User
// can pass uuid.Nil here and will reliably get (nil, nil).
func GetUserSettings(ctx context.Context, db *pg.DB, userID uuid.UUID) (*UserSettings, error) {
	if userID == uuid.Nil {
		return nil, nil
	}
	us := new(UserSettings)
	err := db.Model(us).
		Context(ctx).
		Where("user_id = ?", userID).
		Limit(1).
		Select()
	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return us, nil
}

// UpsertUserSettings writes (or replaces) the row keyed by user_id.
// Idempotent — re-saving the same struct just bumps updated_at via
// the trigger. Toggle-style settings flows should call this on every
// form-submit; partial updates aren't supported because the struct
// is small and "PATCH semantics" would be premature.
func UpsertUserSettings(ctx context.Context, db *pg.DB, us *UserSettings) error {
	if us.UserID == uuid.Nil {
		return errors.New("user_settings: empty user_id")
	}
	if us.CreatedAt.IsZero() {
		us.CreatedAt = time.Now()
	}
	if us.UpdatedAt.IsZero() {
		us.UpdatedAt = us.CreatedAt
	}
	_, err := db.Model(us).
		Context(ctx).
		OnConflict("(user_id) DO UPDATE").
		Set("show_adult = EXCLUDED.show_adult").
		Set("updated_at = now()").
		Insert()
	return err
}

// SetUserSettingsLang records the interface language for an account. Writes
// only that column: the row also carries ShowAdult, and a language change
// must never reset a preference the user set deliberately.
func SetUserSettingsLang(ctx context.Context, db *pg.DB, userID uuid.UUID, lang string) error {
	if userID == uuid.Nil {
		return errors.New("user_settings: empty user_id")
	}
	if lang == "" {
		return nil
	}
	now := time.Now()
	us := &UserSettings{UserID: userID, Lang: &lang, CreatedAt: now, UpdatedAt: now}
	_, err := db.Model(us).
		Context(ctx).
		OnConflict("(user_id) DO UPDATE").
		Set("lang = EXCLUDED.lang").
		Set("updated_at = now()").
		Insert()
	return err
}

// GetLang returns the stored language, or "" when the account has none yet.
func (s *UserSettings) GetLang() string {
	if s == nil || s.Lang == nil {
		return ""
	}
	return *s.Lang
}

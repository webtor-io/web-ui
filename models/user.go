package models

import (
	"context"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"

	uuid "github.com/satori/go.uuid"
)

type User struct {
	tableName     struct{}  `pg:"user"`
	UserID        uuid.UUID `pg:"user_id,pk"`
	Email         string
	Password      string
	PatreonUserID *string `pg:"patreon_user_id"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Tier          string
}

// GetOrCreateUser finds or creates a user.
// If patreonID is provided (non-nil), it first looks up by patreon_member_id.
// - If found, it updates the email if different and returns the user.
// If not found (or patreonID is nil), it falls back to lookup by email.
// - If an email user is found and patreonID is provided but missing on the record, it links it.
// - Otherwise, it creates a new user with provided email (and patreonID if given).
func GetOrCreateUser(ctx context.Context, db *pg.DB, email string, patreonUserID *string) (*User, bool, error) {
	user := &User{}

	// 1) If patreonUserID provided, try to find by patreon_member_id
	if patreonUserID != nil {
		err := db.Model(user).
			Context(ctx).
			Where("patreon_user_id = ?", patreonUserID).
			Limit(1).
			Select()
		if err == nil {
			// Update email if it differs
			if user.Email != email && email != "" {
				user.Email = email
				if _, uerr := db.Model(user).
					Context(ctx).
					Column("email").
					WherePK().
					Update(); uerr != nil {
					return nil, false, uerr
				}
			}
			return user, false, nil
		}
		if !errors.Is(err, pg.ErrNoRows) {
			return nil, false, err
		}
	}

	// 2) Fallback: find by email
	err := db.Model(user).
		Context(ctx).
		Where("email = ?", email).
		Limit(1).
		Select()
	if err == nil {
		// Link patreonUserID if provided and not already set
		if patreonUserID != nil && (user.PatreonUserID == nil || *user.PatreonUserID != *patreonUserID) {
			user.PatreonUserID = patreonUserID
			if _, uerr := db.Model(user).
				Context(ctx).
				Column("patreon_user_id").
				WherePK().
				Update(); uerr != nil {
				return nil, false, uerr
			}
		}
		return user, false, nil
	}
	if !errors.Is(err, pg.ErrNoRows) {
		return nil, false, err // DB error
	}

	// 3) Create new user
	user.Email = email
	user.PatreonUserID = patreonUserID
	_, err = db.Model(user).
		Context(ctx).
		Insert()
	if err != nil {
		return nil, false, err
	}
	return user, true, nil
}

func UpdateUserTier(ctx context.Context, db *pg.DB, u *User) error {
	_, err := db.Model(u).
		Context(ctx).
		WherePK().
		Column("tier").
		Update()
	return err
}

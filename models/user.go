package models

import (
	"context"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"

	uuid "github.com/satori/go.uuid"
)

type User struct {
	tableName struct{}  `pg:"user"`
	UserID    uuid.UUID `pg:"user_id,pk"`
	// Email is identity, in both deployments -- never a contact preference.
	// On webtor.io it is what services/claims keys the tier lookup on and
	// what GetOrCreateUser matches Patreon accounts by. In self-hosted it is
	// the literal string "admin", and services/adminauth plus
	// services/auth.registerAdminUser look up that row by
	// `email = 'admin'` on every single request (there is no session-
	// carried user id in that path). Changing Email out from under either
	// mechanism breaks it silently -- see NotificationEmail for where a
	// user-supplied address actually belongs.
	Email string
	// Password holds the argon2id hash of the self-hosted administrator's
	// password (services/adminauth). It is empty for every other user and on
	// every SuperTokens-backed deployment.
	Password      string
	PatreonUserID *string `pg:"patreon_user_id"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Tier          string
	// NotificationEmail (migration 71) is the confirmed address mail
	// actually goes to, kept deliberately separate from Email/identity --
	// see the comment on Email for why the two must never merge into one
	// column. Nil until a pending address is verified (VerifyPendingEmail).
	// services/notification.RecipientEmail is what picks between this and
	// Email for an outgoing send; read that instead of this field directly.
	NotificationEmail *string `pg:"notification_email"`
	// PendingEmail, PendingEmailToken and PendingEmailExpiresAt (migration
	// 71) carry an address awaiting verification -- see SetPendingEmail and
	// VerifyPendingEmail. All three are nil outside that window: entering a
	// new address overwrites any previous pending one rather than
	// accumulating, and a successful or expired verification clears all
	// three.
	PendingEmail          *string    `pg:"pending_email"`
	PendingEmailToken     *string    `pg:"pending_email_token"`
	PendingEmailExpiresAt *time.Time `pg:"pending_email_expires_at"`
}

// SetPendingEmail stores an address awaiting verification for userID, together
// with the single-use token that confirms it and the token's expiry.
// Deliverability is the caller's job (notification.Deliverable) -- this
// function stores whatever it is given, which is why the handler must check
// first. A second call before the first is confirmed replaces the pending
// state outright (old token included), so a stale link a user forgot about
// simply stops working rather than staying live alongside a newer one.
func SetPendingEmail(ctx context.Context, db *pg.DB, userID uuid.UUID, email, token string, expiresAt time.Time) error {
	_, err := db.Model((*User)(nil)).
		Context(ctx).
		Set("pending_email = ?", email).
		Set("pending_email_token = ?", token).
		Set("pending_email_expires_at = ?", expiresAt).
		Where("user_id = ?", userID).
		Update()
	if err != nil {
		return errors.Wrap(err, "failed to store pending email")
	}
	return nil
}

// VerifyPendingEmail promotes the pending address owned by token to that
// row's notification_email, provided the token has not expired, and clears
// all three pending columns. Returns (false, nil) for a token that matches
// no row or whose row has expired -- the caller cannot and must not
// distinguish the two: telling an unauthenticated GET which one is true
// would leak whether a guessed token was ever valid.
//
// This writes notification_email, never email, and that is not a detail to
// "clean up" later: email is identity on both deployments (see the doc
// comment on User.Email). Concretely, in self-hosted, the admin row's email
// is the literal string "admin", which services/adminauth and
// services/auth.registerAdminUser look up on every request; overwriting it
// here would make that lookup miss on the very next request, which falls
// through to creating a second, empty-password "admin" row and silently
// orphans the operator's account (this happened -- see
// TestGetOrCreateUserAdminLookupSurvivesEmailVerification's negative
// control). On webtor.io the same column feeds the tier lookup and the
// Patreon match. Two separate columns is the fix, not a workaround.
//
// Matching is by token alone, deliberately not combined with a caller-
// supplied user id: the partial unique index on pending_email_token
// (migration 71, WHERE pending_email_token IS NOT NULL) guarantees at most
// one row can hold a given token at a time, so `WHERE pending_email_token =
// token` already can only ever touch the row that owns it. That is what
// makes this safe to call with nothing but the token -- a token belonging to
// user A can only ever promote user A's row, never user B's pending address,
// regardless of who is asking or what user B's own pending state looks
// like. Do not loosen the WHERE clause (e.g. to just the expiry check) to
// "simplify" this: without the token equality, the UPDATE matches every row
// with an unexpired pending address, promoting all of them at once.
func VerifyPendingEmail(ctx context.Context, db *pg.DB, token string) (bool, error) {
	if token == "" {
		return false, nil
	}
	res, err := db.Model((*User)(nil)).
		Context(ctx).
		Set("notification_email = pending_email").
		Set("pending_email = NULL").
		Set("pending_email_token = NULL").
		Set("pending_email_expires_at = NULL").
		Where("pending_email_token = ?", token).
		Where("pending_email_expires_at > now()").
		Update()
	if err != nil {
		return false, errors.Wrap(err, "failed to verify pending email")
	}
	return res.RowsAffected() > 0, nil
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

// GetUserByID loads a user record by primary key. Returns (nil, nil) when no
// row is found so callers can distinguish "missing" from a DB error.
func GetUserByID(ctx context.Context, db *pg.DB, userID uuid.UUID) (*User, error) {
	u := &User{}
	err := db.Model(u).
		Context(ctx).
		Where("user_id = ?", userID).
		Limit(1).
		Select()
	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to load user by id")
	}
	return u, nil
}

func DeleteUser(ctx context.Context, db *pg.DB, userID uuid.UUID) error {
	_, err := db.Model((*User)(nil)).
		Context(ctx).
		Where("user_id = ?", userID).
		Delete()
	if err != nil {
		return errors.Wrap(err, "failed to delete user")
	}
	return nil
}

func UpdateUserTier(ctx context.Context, db *pg.DB, u *User) error {
	_, err := db.Model(u).
		Context(ctx).
		WherePK().
		Column("tier").
		Update()
	return err
}

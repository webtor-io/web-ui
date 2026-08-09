package models

import (
	"context"
	"errors"
	"time"

	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
)

// DeviceAuth is one in-flight device authorization (RFC 8628-style): a device
// holds the secret DeviceCode and polls with it; a person holds the short
// UserCode and confirms it in a browser session. Rows are transient — minutes
// of life, deleted on delivery — which is why expired ones are purged
// opportunistically rather than by a scheduled job.
type DeviceAuth struct {
	tableName struct{} `pg:"device_auth"`

	DeviceCode uuid.UUID  `pg:"device_code,pk"`
	UserCode   string     `pg:"user_code,notnull"`
	UserID     *uuid.UUID `pg:"user_id"`
	// Token is the issued API key, parked here between confirmation and the
	// device's next poll. Delivered exactly once: the row is deleted in the
	// same transaction that returns it.
	Token      *uuid.UUID `pg:"token"`
	DeviceName string     `pg:"device_name"`
	Status     string     `pg:"status,notnull"`
	ExpiresAt  time.Time  `pg:"expires_at,notnull"`
	CreatedAt  time.Time  `pg:"created_at,notnull"`
	UpdatedAt  time.Time  `pg:"updated_at,notnull"`
}

const (
	DeviceAuthPending   = "pending"
	DeviceAuthConfirmed = "confirmed"
)

// CreateDeviceAuth inserts a new pending authorization. A user_code collision
// (the code space is small on purpose — a person types it) surfaces as pg's
// unique violation; the caller retries with a fresh code.
func CreateDeviceAuth(ctx context.Context, db *pg.DB, userCode string, deviceName string, ttl time.Duration) (*DeviceAuth, error) {
	// Purge is piggybacked here rather than scheduled: rows only accumulate
	// while codes are being requested, so this is exactly when cleaning pays.
	if _, err := db.Model((*DeviceAuth)(nil)).Context(ctx).
		Where("expires_at < now()").Delete(); err != nil {
		return nil, err
	}
	da := &DeviceAuth{
		DeviceCode: uuid.NewV4(),
		UserCode:   userCode,
		DeviceName: deviceName,
		Status:     DeviceAuthPending,
		ExpiresAt:  time.Now().Add(ttl),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	if _, err := db.Model(da).Context(ctx).Insert(); err != nil {
		return nil, err
	}
	return da, nil
}

// GetDeviceAuthByUserCode resolves the code a person typed. Only live pending
// rows count — an expired or already-confirmed code must read as unknown, not
// as confirmable again.
func GetDeviceAuthByUserCode(ctx context.Context, db *pg.DB, userCode string) (*DeviceAuth, error) {
	da := new(DeviceAuth)
	err := db.Model(da).Context(ctx).
		Where("user_code = ?", userCode).
		Where("status = ?", DeviceAuthPending).
		Where("expires_at > now()").
		Select()
	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return da, nil
}

// ConfirmDeviceAuth binds the authorization to the confirming user and parks
// the issued token for pickup. Guarded by status: two racing confirmations
// cannot both succeed.
func ConfirmDeviceAuth(ctx context.Context, db *pg.DB, deviceCode uuid.UUID, userID uuid.UUID, token uuid.UUID) (bool, error) {
	res, err := db.Model((*DeviceAuth)(nil)).Context(ctx).
		Set("user_id = ?", userID).
		Set("token = ?", token).
		Set("status = ?", DeviceAuthConfirmed).
		Where("device_code = ?", deviceCode).
		Where("status = ?", DeviceAuthPending).
		Where("expires_at > now()").
		Update()
	if err != nil {
		return false, err
	}
	return res.RowsAffected() == 1, nil
}

// TakeDeviceAuth is the device's poll: it reads the row and, when the token is
// ready, deletes the row in the same transaction — the key is handed out
// exactly once, and a replayed poll reads as an unknown code.
func TakeDeviceAuth(ctx context.Context, db *pg.DB, deviceCode uuid.UUID) (*DeviceAuth, error) {
	var out *DeviceAuth
	err := db.RunInTransaction(ctx, func(tx *pg.Tx) error {
		da := new(DeviceAuth)
		err := tx.Model(da).Context(ctx).
			Where("device_code = ?", deviceCode).
			Where("expires_at > now()").
			For("UPDATE").
			Select()
		if err != nil {
			if errors.Is(err, pg.ErrNoRows) {
				return nil
			}
			return err
		}
		if da.Status == DeviceAuthConfirmed && da.Token != nil {
			if _, err := tx.Model((*DeviceAuth)(nil)).
				Where("device_code = ?", deviceCode).Delete(); err != nil {
				return err
			}
		}
		out = da
		return nil
	})
	return out, err
}

// ListUserDeviceAuth returns the user's in-flight authorizations — the GDPR
// export's view of this table. Almost always empty: rows are minutes-lived.
func ListUserDeviceAuth(ctx context.Context, db *pg.DB, userID uuid.UUID) ([]*DeviceAuth, error) {
	var list []*DeviceAuth
	err := db.Model(&list).Context(ctx).
		Where("user_id = ?", userID).
		OrderExpr("created_at ASC").
		Select()
	if err != nil {
		return nil, err
	}
	return list, nil
}

package models

import (
	"context"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
)

type StremioAddonUrl struct {
	tableName struct{}  `pg:"stremio_addon_url"`
	ID        uuid.UUID `pg:"stremio_addon_url_id,pk,type:uuid,default:uuid_generate_v4()"`
	Url       string    `pg:"url,notnull"`
	CreatedAt time.Time
	UpdatedAt time.Time

	UserID uuid.UUID `pg:"user_id"`
	User   *User     `pg:"rel:has-one,fk:user_id"`
}

// GetUserStremioAddonUrls returns all stremio addon URLs for a specific user
func GetUserStremioAddonUrls(ctx context.Context, db *pg.DB, userID uuid.UUID) ([]StremioAddonUrl, error) {
	var stremioAddonUrls []StremioAddonUrl
	err := db.Model(&stremioAddonUrls).
		Context(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Select()
	if err != nil {
		return nil, err
	}
	return stremioAddonUrls, nil
}

// CountUserStremioAddonUrls returns the number of stremio addon URLs for a specific user
func CountUserStremioAddonUrls(ctx context.Context, db *pg.DB, userID uuid.UUID) (int, error) {
	return db.Model(&StremioAddonUrl{}).
		Context(ctx).
		Where("user_id = ?", userID).
		Count()
}

// StremioAddonUrlExists checks if a stremio addon URL already exists in the system
func StremioAddonUrlExists(ctx context.Context, db *pg.DB, userID uuid.UUID, url string) (bool, error) {
	existing := &StremioAddonUrl{}
	err := db.Model(existing).
		Context(ctx).
		Where("url = ? AND user_id = ?", url, userID).
		Select()
	if err == nil {
		return true, nil
	} else if errors.Is(err, pg.ErrNoRows) {
		return false, nil
	}
	return false, err
}

// CreateStremioAddonUrl creates a new stremio addon URL for a user
func CreateStremioAddonUrl(ctx context.Context, db *pg.DB, userID uuid.UUID, url string) error {
	stremioAddonUrl := &StremioAddonUrl{
		Url:    url,
		UserID: userID,
	}

	_, err := db.Model(stremioAddonUrl).
		Context(ctx).
		Insert()
	return err
}

// DeleteUserStremioAddonUrl deletes a stremio addon URL owned by a specific user
func DeleteUserStremioAddonUrl(ctx context.Context, db *pg.DB, stremioAddonUrlID uuid.UUID, userID uuid.UUID) error {
	_, err := db.Model(&StremioAddonUrl{}).
		Context(ctx).
		Where("stremio_addon_url_id = ? AND user_id = ?", stremioAddonUrlID, userID).
		Delete()
	return err
}

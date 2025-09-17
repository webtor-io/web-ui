package models

import (
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
)

type AddonUrl struct {
	tableName struct{}  `pg:"addon_url"`
	ID        uuid.UUID `pg:"addon_url_id,pk,type:uuid,default:uuid_generate_v4()"`
	Url       string    `pg:"url,notnull"`
	CreatedAt time.Time
	UpdatedAt time.Time

	UserID uuid.UUID `pg:"user_id"`
	User   *User     `pg:"rel:has-one,fk:user_id"`
}

// GetUserAddonUrls returns all addon URLs for a specific user
func GetUserAddonUrls(db *pg.DB, userID uuid.UUID) ([]AddonUrl, error) {
	var addonUrls []AddonUrl
	err := db.Model(&addonUrls).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Select()
	if err != nil {
		return nil, err
	}
	return addonUrls, nil
}

// CountUserAddonUrls returns the number of addon URLs for a specific user
func CountUserAddonUrls(db *pg.DB, userID uuid.UUID) (int, error) {
	return db.Model(&AddonUrl{}).
		Where("user_id = ?", userID).
		Count()
}

// AddonUrlExists checks if an addon URL already exists in the system
func AddonUrlExists(db *pg.DB, url string) (bool, error) {
	existing := &AddonUrl{}
	err := db.Model(existing).Where("url = ?", url).Select()
	if err == nil {
		return true, nil
	} else if errors.Is(err, pg.ErrNoRows) {
		return false, nil
	}
	return false, err
}

// CreateAddonUrl creates a new addon URL for a user
func CreateAddonUrl(db *pg.DB, userID uuid.UUID, url string) error {
	addonUrl := &AddonUrl{
		Url:    url,
		UserID: userID,
	}

	_, err := db.Model(addonUrl).Insert()
	return err
}

// DeleteUserAddonUrl deletes an addon URL owned by a specific user
func DeleteUserAddonUrl(db *pg.DB, addonUrlID uuid.UUID, userID uuid.UUID) error {
	_, err := db.Model(&AddonUrl{}).
		Where("addon_url_id = ? AND user_id = ?", addonUrlID, userID).
		Delete()
	return err
}

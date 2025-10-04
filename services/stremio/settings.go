package stremio

import (
	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/claims"
)

// GetUserStremioSettings returns Stremio settings for a specific user
func GetUserSettingsDataByClaims(db *pg.DB, userID uuid.UUID, cla *claims.Data) (*models.StremioSettingsData, error) {
	s, err := models.GetUserStremioSettingsData(db, userID)
	if err != nil {
		return nil, err
	}
	return s, nil
}

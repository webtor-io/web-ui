package stremio

import (
	"context"

	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models"
)

// GetUserStremioSettings returns Stremio settings for a specific user
func GetUserSettingsDataByClaims(ctx context.Context, db *pg.DB, userID uuid.UUID) (*models.StremioSettingsData, error) {
	s, err := models.GetUserStremioSettingsData(ctx, db, userID)
	if err != nil {
		return nil, err
	}
	return s, nil
}

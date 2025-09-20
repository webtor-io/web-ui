package stremio

import (
	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/claims"
)

const MinBitrateMBpsFor4K = 30

func Is4KAvailable(cla *claims.Data) bool {
	return cla == nil ||
		cla.GetClaims() == nil ||
		cla.GetClaims().GetConnection() == nil ||
		cla.GetClaims().GetConnection().GetRate() == 0 ||
		cla.GetClaims().GetConnection().GetRate() > MinBitrateMBpsFor4K
}

// GetUserStremioSettings returns Stremio settings for a specific user
func GetUserSettingsDataByClaims(db *pg.DB, userID uuid.UUID, cla *claims.Data) (*models.StremioSettingsData, error) {
	s, err := models.GetUserStremioSettingsData(db, userID)
	if err != nil {
		return nil, err
	}
	if Is4KAvailable(cla) {
		return s, nil
	}
	for i, quality := range s.PreferredResolutions {
		if quality.Resolution == "4k" {
			s.PreferredResolutions[i].Enabled = false
			break
		}
	}
	return s, nil
}

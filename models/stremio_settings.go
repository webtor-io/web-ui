package models

import (
	"context"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
)

// ResolutionSetting represents a single quality preference
type ResolutionSetting struct {
	Resolution string `json:"resolution"`
	Enabled    bool   `json:"enabled"`
}

// StremioSettingsData represents the structure of the JSONB settings field
type StremioSettingsData struct {
	PreferredResolutions []ResolutionSetting `json:"preferred_resolutions"`
}

type StremioSettings struct {
	tableName struct{}             `pg:"stremio_settings"`
	ID        uuid.UUID            `pg:"stremio_settings_id,pk,type:uuid,default:uuid_generate_v4()"`
	Settings  *StremioSettingsData `pg:"settings,type:jsonb,notnull"`
	CreatedAt time.Time
	UpdatedAt time.Time

	UserID uuid.UUID `pg:"user_id,unique"`
	User   *User     `pg:"rel:has-one,fk:user_id"`
}

// GetUserStremioSettings returns Stremio settings for a specific user
func GetUserStremioSettings(ctx context.Context, db *pg.DB, userID uuid.UUID) (*StremioSettings, error) {
	settings := &StremioSettings{}
	err := db.Model(settings).
		Context(ctx).
		Where("user_id = ?", userID).
		Select()
	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return settings, nil
}

// GetUserStremioSettings returns Stremio settings for a specific user
func GetUserStremioSettingsData(ctx context.Context, db *pg.DB, userID uuid.UUID) (*StremioSettingsData, error) {
	s, err := GetUserStremioSettings(ctx, db, userID)
	if err != nil {
		return nil, err
	}
	if s == nil {
		return GetDefaultStremioSettings(), nil
	}
	return s.Settings, nil
}

// CreateStremioSettings creates new Stremio settings for a user
func CreateStremioSettings(ctx context.Context, db *pg.DB, userID uuid.UUID, settings *StremioSettingsData) error {
	stremioSettings := &StremioSettings{
		UserID:   userID,
		Settings: settings,
	}

	_, err := db.Model(stremioSettings).
		Context(ctx).
		Insert()
	return err
}

// UpdateStremioSettings updates Stremio settings for a user
func UpdateStremioSettings(ctx context.Context, db *pg.DB, userID uuid.UUID, settings *StremioSettingsData) error {
	_, err := db.Model(&StremioSettings{}).
		Context(ctx).
		Set("settings = ?", settings).
		Where("user_id = ?", userID).
		Update()
	return err
}

// CreateOrUpdateStremioSettings creates or updates Stremio settings for a user
func CreateOrUpdateStremioSettings(ctx context.Context, db *pg.DB, userID uuid.UUID, settings *StremioSettingsData) error {
	existing, err := GetUserStremioSettings(ctx, db, userID)
	if err != nil {
		return err
	}

	if existing == nil {
		return CreateStremioSettings(ctx, db, userID, settings)
	}

	return UpdateStremioSettings(ctx, db, userID, settings)
}

// GetDefaultStremioSettings returns the default Stremio settings
func GetDefaultStremioSettings() *StremioSettingsData {
	return &StremioSettingsData{
		PreferredResolutions: []ResolutionSetting{
			{Resolution: "4k", Enabled: false},
			{Resolution: "1080p", Enabled: true},
			{Resolution: "720p", Enabled: true},
			{Resolution: "other", Enabled: true},
		},
	}
}

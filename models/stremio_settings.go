package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
)

// QualitySetting represents a single quality preference
type QualitySetting struct {
	Quality string `json:"quality"`
	Enabled bool   `json:"enabled"`
}

// StremioSettingsData represents the structure of the JSONB settings field
type StremioSettingsData struct {
	PreferredQualities []QualitySetting `json:"preferred_qualities"`
}

// Value implements the driver.Valuer interface for database serialization
func (s StremioSettingsData) Value() (driver.Value, error) {
	return json.Marshal(s)
}

// Scan implements the sql.Scanner interface for database deserialization
func (s *StremioSettingsData) Scan(value interface{}) error {
	if value == nil {
		return nil
	}

	var data []byte
	switch v := value.(type) {
	case string:
		data = []byte(v)
	case []byte:
		data = v
	default:
		return fmt.Errorf("cannot scan StremioSettingsData from non-string/non-bytes value: %T", value)
	}

	return json.Unmarshal(data, s)
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
func GetUserStremioSettings(db *pg.DB, userID uuid.UUID) (*StremioSettings, error) {
	settings := &StremioSettings{}
	err := db.Model(settings).
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
func GetUserStremioSettingsData(db *pg.DB, userID uuid.UUID) (*StremioSettingsData, error) {
	s, err := GetUserStremioSettings(db, userID)
	if err != nil {
		return nil, err
	}
	if s == nil {
		return GetDefaultStremioSettings(), nil
	}
	return s.Settings, nil
}

// CreateStremioSettings creates new Stremio settings for a user
func CreateStremioSettings(db *pg.DB, userID uuid.UUID, settings *StremioSettingsData) error {
	stremioSettings := &StremioSettings{
		UserID:   userID,
		Settings: settings,
	}

	_, err := db.Model(stremioSettings).Insert()
	return err
}

// UpdateStremioSettings updates Stremio settings for a user
func UpdateStremioSettings(db *pg.DB, userID uuid.UUID, settings *StremioSettingsData) error {
	_, err := db.Model(&StremioSettings{}).
		Set("settings = ?", settings).
		Where("user_id = ?", userID).
		Update()
	return err
}

// CreateOrUpdateStremioSettings creates or updates Stremio settings for a user
func CreateOrUpdateStremioSettings(db *pg.DB, userID uuid.UUID, settings *StremioSettingsData) error {
	existing, err := GetUserStremioSettings(db, userID)
	if err != nil {
		return err
	}

	if existing == nil {
		return CreateStremioSettings(db, userID, settings)
	}

	return UpdateStremioSettings(db, userID, settings)
}

// GetDefaultStremioSettings returns the default Stremio settings
func GetDefaultStremioSettings() *StremioSettingsData {
	return &StremioSettingsData{
		PreferredQualities: []QualitySetting{
			{Quality: "4k", Enabled: false},
			{Quality: "1080p", Enabled: true},
			{Quality: "720p", Enabled: true},
			{Quality: "other", Enabled: true},
		},
	}
}

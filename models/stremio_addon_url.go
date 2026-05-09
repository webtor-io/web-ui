package models

import (
	"context"
	"encoding/json"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
)

type StremioAddonUrl struct {
	tableName struct{}  `pg:"stremio_addon_url"`
	ID        uuid.UUID `pg:"stremio_addon_url_id,pk,type:uuid,default:uuid_generate_v4()"`
	Url       string    `pg:"url,notnull"`
	Priority  int16     `pg:"priority,notnull,default:1"`
	Enabled   bool      `pg:"enabled,notnull,default:true,use_zero"`
	CreatedAt time.Time
	UpdatedAt time.Time

	// Manifest snapshot — captured on add and refreshed via the
	// /refresh-snapshot endpoint. Nullable: pre-existing rows from before
	// migration #52 carry NULLs until they're refreshed lazily by the
	// Discover client or manually from the profile.
	ManifestID        *string    `pg:"manifest_id"`
	Name              *string    `pg:"name"`
	ManifestVersion   *string    `pg:"manifest_version"`
	ManifestResources []string   `pg:"manifest_resources,type:jsonb"`
	ManifestTypes     []string   `pg:"manifest_types,type:jsonb"`
	ManifestLogo      *string    `pg:"manifest_logo"`
	ManifestFetchedAt *time.Time `pg:"manifest_fetched_at"`

	UserID uuid.UUID `pg:"user_id"`
	User   *User     `pg:"rel:has-one,fk:user_id"`
}

// ManifestSnapshot is the subset of manifest fields we persist alongside
// the addon URL. Used by the validator (on add) and the refresh endpoint.
type ManifestSnapshot struct {
	ID        string
	Name      string
	Version   string
	Resources []string
	Types     []string
	Logo      string
}

// ApplyManifestSnapshot copies snapshot fields into the model and sets
// ManifestFetchedAt to now. Use before Update().
func (a *StremioAddonUrl) ApplyManifestSnapshot(s *ManifestSnapshot) {
	if s == nil {
		return
	}
	now := time.Now()
	a.ManifestID = strPtr(s.ID)
	a.Name = strPtr(s.Name)
	a.ManifestVersion = strPtr(s.Version)
	a.ManifestResources = append([]string(nil), s.Resources...)
	a.ManifestTypes = append([]string(nil), s.Types...)
	a.ManifestLogo = strPtr(s.Logo)
	a.ManifestFetchedAt = &now
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// GetUserStremioAddonUrls returns enabled stremio addon URLs for a specific user, ordered by priority
func GetUserStremioAddonUrls(ctx context.Context, db *pg.DB, userID uuid.UUID) ([]StremioAddonUrl, error) {
	var stremioAddonUrls []StremioAddonUrl
	err := db.Model(&stremioAddonUrls).
		Context(ctx).
		Where("user_id = ? AND enabled = ?", userID, true).
		Order("priority DESC").
		Select()
	if err != nil {
		return nil, err
	}
	return stremioAddonUrls, nil
}

// GetAllUserStremioAddonUrls returns all stremio addon URLs for a specific user (enabled and disabled), ordered by priority
func GetAllUserStremioAddonUrls(ctx context.Context, db *pg.DB, userID uuid.UUID) ([]StremioAddonUrl, error) {
	var stremioAddonUrls []StremioAddonUrl
	err := db.Model(&stremioAddonUrls).
		Context(ctx).
		Where("user_id = ?", userID).
		Order("priority DESC").
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

// GetStremioAddonUrlByID retrieves a stremio addon URL by ID
func GetStremioAddonUrlByID(ctx context.Context, db *pg.DB, addonID uuid.UUID) (*StremioAddonUrl, error) {
	addon := &StremioAddonUrl{}
	err := db.Model(addon).
		Context(ctx).
		Where("stremio_addon_url_id = ?", addonID).
		Select()
	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return addon, nil
}

// CreateStremioAddonUrl creates a new stremio addon URL for a user. When
// snapshot is non-nil the manifest fields are populated at insert time so
// the UI can render addon name/capabilities without waiting on a fresh
// client-side manifest fetch.
func CreateStremioAddonUrl(ctx context.Context, db *pg.DB, userID uuid.UUID, url string, snapshot *ManifestSnapshot) error {
	// Get current addon count to set priority (new addons get lowest priority)
	count, err := CountUserStremioAddonUrls(ctx, db, userID)
	if err != nil {
		return err
	}

	stremioAddonUrl := &StremioAddonUrl{
		Url:      url,
		UserID:   userID,
		Priority: int16(count + 1), // New addon gets lowest priority
		Enabled:  true,
	}
	stremioAddonUrl.ApplyManifestSnapshot(snapshot)

	_, err = db.Model(stremioAddonUrl).
		Context(ctx).
		Insert()
	return err
}

// UpdateStremioAddonUrl updates a stremio addon URL
func UpdateStremioAddonUrl(ctx context.Context, db *pg.DB, addon *StremioAddonUrl) error {
	_, err := db.Model(addon).
		Context(ctx).
		WherePK().
		Update()
	return err
}

// UpdateStremioAddonUrlSnapshot refreshes the manifest snapshot columns
// for a single addon owned by the given user. Used by the lazy backfill
// from the Discover client and the manual refresh button in the profile.
// Returns pg.ErrNoRows if the addon doesn't exist or belongs to someone
// else.
func UpdateStremioAddonUrlSnapshot(ctx context.Context, db *pg.DB, addonID, userID uuid.UUID, snapshot *ManifestSnapshot) error {
	if snapshot == nil {
		return errors.New("snapshot is required")
	}
	now := time.Now()
	res, err := db.Model(&StremioAddonUrl{}).
		Context(ctx).
		Set("manifest_id = ?", strPtr(snapshot.ID)).
		Set("name = ?", strPtr(snapshot.Name)).
		Set("manifest_version = ?", strPtr(snapshot.Version)).
		Set("manifest_resources = ?", jsonbStringSlice(snapshot.Resources)).
		Set("manifest_types = ?", jsonbStringSlice(snapshot.Types)).
		Set("manifest_logo = ?", strPtr(snapshot.Logo)).
		Set("manifest_fetched_at = ?", now).
		Where("stremio_addon_url_id = ? AND user_id = ?", addonID, userID).
		Update()
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return pg.ErrNoRows
	}
	return nil
}

// jsonbStringSlice marshals a []string as a JSONB literal for go-pg's Set
// builder. Returning a raw []string here would be ambiguous between SQL
// array and JSONB; explicit json.Marshal removes that ambiguity. Nil
// input becomes a typed-nil json.RawMessage, which go-pg renders as SQL
// NULL — exactly what we want for the "never seen" sentinel.
func jsonbStringSlice(s []string) interface{} {
	if s == nil {
		return json.RawMessage(nil)
	}
	b, err := json.Marshal(s)
	if err != nil {
		return json.RawMessage(nil)
	}
	return json.RawMessage(b)
}

// DeleteUserStremioAddonUrl deletes a stremio addon URL owned by a specific user
func DeleteUserStremioAddonUrl(ctx context.Context, db *pg.DB, stremioAddonUrlID uuid.UUID, userID uuid.UUID) error {
	_, err := db.Model(&StremioAddonUrl{}).
		Context(ctx).
		Where("stremio_addon_url_id = ? AND user_id = ?", stremioAddonUrlID, userID).
		Delete()
	return err
}

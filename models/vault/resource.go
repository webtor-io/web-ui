package vault

import (
	"context"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
)

type Resource struct {
	tableName  struct{}   `pg:"vault.resource"`
	ResourceID string     `pg:"resource_id,pk"`
	RequiredVP float64    `pg:"required_vp,notnull,type:numeric"`
	FundedVP   float64    `pg:"funded_vp,notnull,type:numeric"`
	Funded     bool       `pg:"funded,notnull,default:false"`
	Vaulted    bool       `pg:"vaulted,notnull,default:false"`
	FundedAt   *time.Time `pg:"funded_at"`
	VaultedAt  *time.Time `pg:"vaulted_at"`
	Expired    bool       `pg:"expired,notnull,default:false"`
	ExpiredAt  *time.Time `pg:"expired_at"`
	Name       string     `pg:"name,notnull"`
	CreatedAt  time.Time  `pg:"created_at,notnull,default:now()"`
	UpdatedAt  time.Time  `pg:"updated_at,notnull,default:now()"`
}

// GetResource returns a resource by ID
func GetResource(ctx context.Context, db *pg.DB, resourceID string) (*Resource, error) {
	resource := &Resource{}
	err := db.Model(resource).
		Context(ctx).
		Where("resource_id = ?", resourceID).
		Select()
	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to get resource")
	}
	return resource, nil
}

// CreateResource creates a new resource
func CreateResource(ctx context.Context, db *pg.DB, resourceID string, requiredVP float64, torrentName string) (*Resource, error) {
	resource := &Resource{
		ResourceID: resourceID,
		RequiredVP: requiredVP,
		FundedVP:   0,
		Funded:     false,
		Vaulted:    false,
		Expired:    false,
		Name:       torrentName,
	}

	_, err := db.Model(resource).
		Context(ctx).
		Insert()
	if err != nil {
		return nil, errors.Wrap(err, "failed to create resource")
	}
	return resource, nil
}

// UpdateResourceFundedVP updates the funded VP amount for a resource
func UpdateResourceFundedVP(ctx context.Context, db pg.DBI, resourceID string, fundedVP float64) error {
	_, err := db.Model(&Resource{}).
		Context(ctx).
		Set("funded_vp = ?", fundedVP).
		Where("resource_id = ?", resourceID).
		Update()
	if err != nil {
		return errors.Wrap(err, "failed to update resource funded VP")
	}
	return nil
}

// MarkResourceVaulted marks a resource as vaulted
func MarkResourceVaulted(ctx context.Context, db *pg.DB, resourceID string) error {
	now := time.Now()
	_, err := db.Model(&Resource{}).
		Context(ctx).
		Set("vaulted = true").
		Set("vaulted_at = ?", now).
		Where("resource_id = ?", resourceID).
		Update()
	if err != nil {
		return errors.Wrap(err, "failed to mark resource as vaulted")
	}
	return nil
}

// UpdateResourceVaulted updates the vaulted status and timestamp for a resource
func UpdateResourceVaulted(ctx context.Context, db pg.DBI, resourceID string) error {
	_, err := db.Model(&Resource{}).
		Context(ctx).
		Set("vaulted = ?", true).
		Set("vaulted_at = ?", time.Now()).
		Where("resource_id = ?", resourceID).Update()
	if err != nil {
		return errors.Wrap(err, "failed to update resource vaulted status")
	}
	return nil
}

// MarkResourceUnexpiredAndFunded marks a resource as not expired and funded
func MarkResourceUnexpiredAndFunded(ctx context.Context, db pg.DBI, resourceID string) error {
	now := time.Now()
	_, err := db.Model(&Resource{}).
		Context(ctx).
		Set("expired = false").
		Set("expired_at = NULL").
		Set("funded = true").
		Set("funded_at = ?", now).
		Where("resource_id = ?", resourceID).
		Update()
	if err != nil {
		return errors.Wrap(err, "failed to mark resource as unexpired")
	}
	return nil
}

// MarkResourceExpiredAndUnfunded marks a resource as expired and unfunded
func MarkResourceExpiredAndUnfunded(ctx context.Context, db pg.DBI, resourceID string) error {
	now := time.Now()
	_, err := db.Model(&Resource{}).
		Context(ctx).
		Set("expired = true").
		Set("expired_at = ?", now).
		Set("funded_at = NULL").
		Set("funded = false").
		Where("resource_id = ?", resourceID).
		Update()
	if err != nil {
		return errors.Wrap(err, "failed to mark resource as expired and unfunded")
	}
	return nil
}

// GetExpiredResources returns resources that should be reaped based on expiration and transfer timeout periods.
// Resources without any pledges (abandoned) expire after abandonedExpirePeriod (1 day by default).
// Resources with unfunded pledges expire after expirePeriod (7 days by default).
func GetExpiredResources(ctx context.Context, db *pg.DB, expirePeriod time.Duration, abandonedExpirePeriod time.Duration, transferTimeoutPeriod time.Duration) ([]Resource, error) {
	var resources []Resource
	now := time.Now()
	expireThreshold := now.Add(-expirePeriod)
	abandonedThreshold := now.Add(-abandonedExpirePeriod)
	transferThreshold := now.Add(-transferTimeoutPeriod)

	err := db.Model(&resources).
		Context(ctx).
		WhereGroup(func(q *pg.Query) (*pg.Query, error) {
			// Expired resources with pledges still attached (unfunded) — wait full expire period
			q = q.WhereOr("expired_at IS NOT NULL AND expired_at < ? AND EXISTS (SELECT 1 FROM vault.pledge WHERE pledge.resource_id = resource.resource_id)", expireThreshold).
				// Expired resources with no pledges (abandoned) — expire faster
				WhereOr("expired_at IS NOT NULL AND expired_at < ? AND NOT EXISTS (SELECT 1 FROM vault.pledge WHERE pledge.resource_id = resource.resource_id)", abandonedThreshold).
				// Transfer timeout — resource funded but never vaulted
				WhereOr("funded_at IS NOT NULL AND funded_at < ? AND vaulted = false", transferThreshold)
			return q, nil
		}).
		Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get expired resources")
	}
	return resources, nil
}

// GetGhostResources returns resources where funded_vp > 0 but no funded pledges exist.
// This happens when a user account is deleted — CASCADE removes pledges but resource
// retains its funded state.
func GetGhostResources(ctx context.Context, db *pg.DB) ([]Resource, error) {
	var resources []Resource
	err := db.Model(&resources).
		Context(ctx).
		Where("funded_vp > 0").
		Where("NOT EXISTS (SELECT 1 FROM vault.pledge WHERE pledge.resource_id = resource.resource_id AND pledge.funded = true)").
		Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get ghost resources")
	}
	return resources, nil
}

// DeleteResource deletes a resource
func DeleteResource(ctx context.Context, db *pg.DB, resourceID string) error {
	_, err := db.Model(&Resource{}).
		Context(ctx).
		Where("resource_id = ?", resourceID).
		Delete()
	if err != nil {
		return errors.Wrap(err, "failed to delete resource")
	}
	return nil
}

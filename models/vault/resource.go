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

// GetExpiredResources returns resources that should be reaped based on expiration and transfer timeout periods
func GetExpiredResources(ctx context.Context, db *pg.DB, expirePeriod time.Duration, transferTimeoutPeriod time.Duration) ([]Resource, error) {
	var resources []Resource
	now := time.Now()
	expireThreshold := now.Add(-expirePeriod)
	transferThreshold := now.Add(-transferTimeoutPeriod)

	err := db.Model(&resources).
		Context(ctx).
		WhereGroup(func(q *pg.Query) (*pg.Query, error) {
			q = q.WhereOr("expired_at IS NOT NULL AND expired_at < ?", expireThreshold).
				WhereOr("funded_at IS NOT NULL AND funded_at < ? AND vaulted = false", transferThreshold)
			return q, nil
		}).
		Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get expired resources")
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

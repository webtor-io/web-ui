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

// GetFundedResources returns all funded resources
func GetFundedResources(ctx context.Context, db *pg.DB) ([]Resource, error) {
	var resources []Resource
	err := db.Model(&resources).
		Context(ctx).
		Where("funded = true").
		Order("funded_at DESC").
		Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get funded resources")
	}
	return resources, nil
}

// GetVaultedResources returns all vaulted resources
func GetVaultedResources(ctx context.Context, db *pg.DB) ([]Resource, error) {
	var resources []Resource
	err := db.Model(&resources).
		Context(ctx).
		Where("vaulted = true").
		Order("vaulted_at DESC").
		Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get vaulted resources")
	}
	return resources, nil
}

// CreateResource creates a new resource
func CreateResource(ctx context.Context, db *pg.DB, resourceID string, requiredVP float64) (*Resource, error) {
	resource := &Resource{
		ResourceID: resourceID,
		RequiredVP: requiredVP,
		FundedVP:   0,
		Funded:     false,
		Vaulted:    false,
		Expired:    false,
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

// MarkResourceFunded marks a resource as funded
func MarkResourceFunded(ctx context.Context, db pg.DBI, resourceID string) error {
	now := time.Now()
	_, err := db.Model(&Resource{}).
		Context(ctx).
		Set("funded = true").
		Set("funded_at = ?", now).
		Where("resource_id = ?", resourceID).
		Update()
	if err != nil {
		return errors.Wrap(err, "failed to mark resource as funded")
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

// MarkResourceExpired marks a resource as expired
func MarkResourceExpired(ctx context.Context, db pg.DBI, resourceID string) error {
	now := time.Now()
	_, err := db.Model(&Resource{}).
		Context(ctx).
		Set("expired = true").
		Set("expired_at = ?", now).
		Where("resource_id = ?", resourceID).
		Update()
	if err != nil {
		return errors.Wrap(err, "failed to mark resource as expired")
	}
	return nil
}

// MarkResourceUnfunded marks a resource as unfunded
func MarkResourceUnfunded(ctx context.Context, db pg.DBI, resourceID string) error {
	_, err := db.Model(&Resource{}).
		Context(ctx).
		Set("funded = false").
		Set("funded_at = NULL").
		Where("resource_id = ?", resourceID).
		Update()
	if err != nil {
		return errors.Wrap(err, "failed to mark resource as unfunded")
	}
	return nil
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

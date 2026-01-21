package vault

import (
	"context"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models"
)

type Pledge struct {
	tableName  struct{}  `pg:"vault.pledge"`
	PledgeID   uuid.UUID `pg:"pledge_id,pk,type:uuid,default:uuid_generate_v4()"`
	ResourceID string    `pg:"resource_id,notnull"`
	UserID     uuid.UUID `pg:"user_id,notnull,type:uuid"`
	Amount     float64   `pg:"amount,notnull,type:numeric"`
	Funded     bool      `pg:"funded,notnull,default:true"`
	Frozen     bool      `pg:"frozen,notnull,default:true"`
	FrozenAt   time.Time `pg:"frozen_at,notnull,default:now()"`
	CreatedAt  time.Time `pg:"created_at,notnull,default:now()"`
	UpdatedAt  time.Time `pg:"updated_at,notnull,default:now()"`

	User *models.User `pg:"rel:has-one,fk:user_id"`
}

// GetPledge returns a pledge by ID
func GetPledge(ctx context.Context, db *pg.DB, pledgeID uuid.UUID) (*Pledge, error) {
	pledge := &Pledge{}
	err := db.Model(pledge).
		Context(ctx).
		Where("pledge_id = ?", pledgeID).
		Select()
	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to get pledge")
	}
	return pledge, nil
}

// GetUserPledges returns all pledges for a specific user
func GetUserPledges(ctx context.Context, db *pg.DB, userID uuid.UUID) ([]Pledge, error) {
	var pledges []Pledge
	err := db.Model(&pledges).
		Context(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get user pledges")
	}
	return pledges, nil
}

// GetResourcePledges returns all pledges for a specific resource
func GetResourcePledges(ctx context.Context, db *pg.DB, resourceID string) ([]Pledge, error) {
	var pledges []Pledge
	err := db.Model(&pledges).
		Context(ctx).
		Where("resource_id = ?", resourceID).
		Order("created_at DESC").
		Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get resource pledges")
	}
	return pledges, nil
}

// GetFundedResourcePledges returns all funded pledges for a specific resource
func GetFundedResourcePledges(ctx context.Context, db *pg.DB, resourceID string) ([]Pledge, error) {
	var pledges []Pledge
	err := db.Model(&pledges).
		Context(ctx).
		Where("resource_id = ? AND funded = true", resourceID).
		Order("created_at DESC").
		Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get funded resource pledges")
	}
	return pledges, nil
}

// CreatePledge creates a new pledge
func CreatePledge(ctx context.Context, db *pg.DB, userID uuid.UUID, resourceID string, amount float64) (*Pledge, error) {
	pledge := &Pledge{
		UserID:     userID,
		ResourceID: resourceID,
		Amount:     amount,
		Funded:     true,
		Frozen:     true,
	}

	_, err := db.Model(pledge).
		Context(ctx).
		Insert()
	if err != nil {
		return nil, errors.Wrap(err, "failed to create pledge")
	}
	return pledge, nil
}

// UpdatePledgeFunded updates the funded status of a pledge
func UpdatePledgeFunded(ctx context.Context, db *pg.DB, pledgeID uuid.UUID, funded bool) error {
	_, err := db.Model(&Pledge{}).
		Context(ctx).
		Set("funded = ?", funded).
		Where("pledge_id = ?", pledgeID).
		Update()
	if err != nil {
		return errors.Wrap(err, "failed to update pledge funded status")
	}
	return nil
}

// UpdatePledgeFrozen updates the frozen status of a pledge
func UpdatePledgeFrozen(ctx context.Context, db *pg.DB, pledgeID uuid.UUID, frozen bool) error {
	_, err := db.Model(&Pledge{}).
		Context(ctx).
		Set("frozen = ?", frozen).
		Where("pledge_id = ?", pledgeID).
		Update()
	if err != nil {
		return errors.Wrap(err, "failed to update pledge frozen status")
	}
	return nil
}

// DeletePledge deletes a pledge
func DeletePledge(ctx context.Context, db *pg.DB, pledgeID uuid.UUID) error {
	_, err := db.Model(&Pledge{}).
		Context(ctx).
		Where("pledge_id = ?", pledgeID).
		Delete()
	if err != nil {
		return errors.Wrap(err, "failed to delete pledge")
	}
	return nil
}

// SumFundedPledgesForResource calculates total funded amount for a resource
func SumFundedPledgesForResource(ctx context.Context, db *pg.DB, resourceID string) (float64, error) {
	var sum float64
	err := db.Model(&Pledge{}).
		Context(ctx).
		ColumnExpr("COALESCE(SUM(amount), 0)").
		Where("resource_id = ? AND funded = true", resourceID).
		Select(&sum)
	if err != nil {
		return 0, errors.Wrap(err, "failed to sum funded pledges")
	}
	return sum, nil
}

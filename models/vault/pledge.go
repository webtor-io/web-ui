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
	FrozenAt   time.Time `pg:"frozen_at,notnull,default:now()"`
	CreatedAt  time.Time `pg:"created_at,notnull,default:now()"`
	UpdatedAt  time.Time `pg:"updated_at,notnull,default:now()"`

	User *models.User `pg:"rel:has-one,fk:user_id"`
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

// GetUserPledgesOrderedByCreation returns all pledges for a specific user ordered by creation time ascending
func GetUserPledgesOrderedByCreation(ctx context.Context, db pg.DBI, userID uuid.UUID) ([]Pledge, error) {
	var pledges []Pledge
	err := db.Model(&pledges).
		Context(ctx).
		Where("user_id = ?", userID).
		Order("created_at ASC").
		Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get user pledges ordered by creation")
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

// GetUserResourcePledge returns a pledge for a specific user and resource
func GetUserResourcePledge(ctx context.Context, db *pg.DB, userID uuid.UUID, resourceID string) (*Pledge, error) {
	pledge := &Pledge{}
	err := db.Model(pledge).
		Context(ctx).
		Where("user_id = ? AND resource_id = ?", userID, resourceID).
		Select()
	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to get user resource pledge")
	}
	return pledge, nil
}

// UpdatePledgeFunded updates the funded status of a pledge
func UpdatePledgeFunded(ctx context.Context, db pg.DBI, pledgeID uuid.UUID, funded bool) error {
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

// DeletePledge deletes a pledge
func DeletePledge(ctx context.Context, db pg.DBI, pledgeID uuid.UUID) error {
	_, err := db.Model(&Pledge{}).
		Context(ctx).
		Where("pledge_id = ?", pledgeID).
		Delete()
	if err != nil {
		return errors.Wrap(err, "failed to delete pledge")
	}
	return nil
}

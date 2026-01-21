package vault

import (
	"context"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models"
)

type UserVP struct {
	tableName struct{}  `pg:"vault.user_vp"`
	UserID    uuid.UUID `pg:"user_id,pk,type:uuid"`
	Total     *float64  `pg:"total,type:numeric"`
	CreatedAt time.Time `pg:"created_at,notnull,default:now()"`
	UpdatedAt time.Time `pg:"updated_at,notnull,default:now()"`

	User *models.User `pg:"rel:has-one,fk:user_id"`
}

// GetUserVP returns vault points for a specific user
func GetUserVP(ctx context.Context, db *pg.DB, userID uuid.UUID) (*UserVP, error) {
	vp := &UserVP{}
	err := db.Model(vp).
		Context(ctx).
		Where("user_id = ?", userID).
		Select()
	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to get user vault points")
	}
	return vp, nil
}

// CreateUserVP creates a new vault points record for a user
func CreateUserVP(ctx context.Context, db *pg.DB, userID uuid.UUID, total *float64) error {
	vp := &UserVP{
		UserID: userID,
		Total:  total,
	}

	_, err := db.Model(vp).
		Context(ctx).
		Insert()
	if err != nil {
		return errors.Wrap(err, "failed to create user vault points")
	}
	return nil
}

// UpdateUserVP updates vault points for a user
func UpdateUserVP(ctx context.Context, db *pg.DB, userID uuid.UUID, total *float64) error {
	_, err := db.Model(&UserVP{}).
		Context(ctx).
		Set("total = ?", total).
		Where("user_id = ?", userID).
		Update()
	if err != nil {
		return errors.Wrap(err, "failed to update user vault points")
	}
	return nil
}

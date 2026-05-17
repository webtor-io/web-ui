package vault

import (
	"context"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models"
)

// Operation types for transaction log
const (
	OpTypeChangeTier   int16 = 1 // Change tier - can be positive or negative
	OpTypeFund         int16 = 2 // Fund pledge - always negative
	OpTypeClaim        int16 = 3 // Claim pledge - always positive
	OpTypeAbuseRefund  int16 = 4 // Refund issued by the abuse-store cleanup flow - always positive
)

type TxLog struct {
	tableName  struct{}  `pg:"vault.tx_log"`
	TxLogID    uuid.UUID `pg:"tx_log_id,pk,type:uuid,default:uuid_generate_v4()"`
	UserID     uuid.UUID `pg:"user_id,notnull,type:uuid"`
	ResourceID *string   `pg:"resource_id"`
	Balance    float64   `pg:"balance,notnull,type:numeric"`
	OpType     int16     `pg:"op_type,notnull"`
	CreatedAt  time.Time `pg:"created_at,notnull,default:now()"`
	UpdatedAt  time.Time `pg:"updated_at,notnull,default:now()"`

	User *models.User `pg:"rel:has-one,fk:user_id"`
}

// ListUserTxLogs returns every vault.tx_log row for a user, oldest first.
// Used by the GDPR data-export.
func ListUserTxLogs(ctx context.Context, db *pg.DB, userID uuid.UUID) ([]TxLog, error) {
	var list []TxLog
	err := db.Model(&list).
		Context(ctx).
		Where("user_id = ?", userID).
		Order("created_at ASC").
		Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to list user tx logs")
	}
	return list, nil
}

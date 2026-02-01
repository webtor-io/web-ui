package vault

import (
	"time"

	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models"
)

// Operation types for transaction log
const (
	OpTypeChangeTier int16 = 1 // Change tier - can be positive or negative
	OpTypeFund       int16 = 2 // Fund pledge - always negative
	OpTypeClaim      int16 = 3 // Claim pledge - always positive
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

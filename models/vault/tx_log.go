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

// GetTxLog returns a transaction log entry by ID
func GetTxLog(ctx context.Context, db *pg.DB, txLogID uuid.UUID) (*TxLog, error) {
	txLog := &TxLog{}
	err := db.Model(txLog).
		Context(ctx).
		Where("tx_log_id = ?", txLogID).
		Select()
	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to get transaction log")
	}
	return txLog, nil
}

// GetUserTxLogs returns all transaction logs for a specific user
func GetUserTxLogs(ctx context.Context, db *pg.DB, userID uuid.UUID) ([]TxLog, error) {
	var logs []TxLog
	err := db.Model(&logs).
		Context(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get user transaction logs")
	}
	return logs, nil
}

// GetUserTxLogsByType returns transaction logs for a user filtered by operation type
func GetUserTxLogsByType(ctx context.Context, db *pg.DB, userID uuid.UUID, opType int16) ([]TxLog, error) {
	var logs []TxLog
	err := db.Model(&logs).
		Context(ctx).
		Where("user_id = ? AND op_type = ?", userID, opType).
		Order("created_at DESC").
		Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get user transaction logs by type")
	}
	return logs, nil
}

// GetResourceTxLogs returns all transaction logs for a specific resource
func GetResourceTxLogs(ctx context.Context, db *pg.DB, resourceID string) ([]TxLog, error) {
	var logs []TxLog
	err := db.Model(&logs).
		Context(ctx).
		Where("resource_id = ?", resourceID).
		Order("created_at DESC").
		Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get resource transaction logs")
	}
	return logs, nil
}

// CreateTxLog creates a new transaction log entry
func CreateTxLog(ctx context.Context, db *pg.DB, userID uuid.UUID, resourceID *string, balance float64, opType int16) (*TxLog, error) {
	txLog := &TxLog{
		UserID:     userID,
		ResourceID: resourceID,
		Balance:    balance,
		OpType:     opType,
	}

	_, err := db.Model(txLog).
		Context(ctx).
		Insert()
	if err != nil {
		return nil, errors.Wrap(err, "failed to create transaction log")
	}
	return txLog, nil
}

// CreateChangeTierLog creates a transaction log for tier change
func CreateChangeTierLog(ctx context.Context, db *pg.DB, userID uuid.UUID, balance float64) (*TxLog, error) {
	return CreateTxLog(ctx, db, userID, nil, balance, OpTypeChangeTier)
}

// CreateFundLog creates a transaction log for funding a pledge
func CreateFundLog(ctx context.Context, db *pg.DB, userID uuid.UUID, resourceID string, balance float64) (*TxLog, error) {
	return CreateTxLog(ctx, db, userID, &resourceID, balance, OpTypeFund)
}

// CreateClaimLog creates a transaction log for claiming a pledge
func CreateClaimLog(ctx context.Context, db *pg.DB, userID uuid.UUID, resourceID string, balance float64) (*TxLog, error) {
	return CreateTxLog(ctx, db, userID, &resourceID, balance, OpTypeClaim)
}

// GetUserBalanceSum calculates the total balance for a user from transaction logs
func GetUserBalanceSum(ctx context.Context, db *pg.DB, userID uuid.UUID) (float64, error) {
	var sum float64
	err := db.Model(&TxLog{}).
		Context(ctx).
		ColumnExpr("COALESCE(SUM(balance), 0)").
		Where("user_id = ?", userID).
		Select(&sum)
	if err != nil {
		return 0, errors.Wrap(err, "failed to calculate user balance sum")
	}
	return sum, nil
}

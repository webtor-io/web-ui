package models

import (
	"context"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
)

type SpeedtestResult struct {
	tableName         struct{}  `pg:"speedtest_result"`
	SpeedtestResultID uuid.UUID `pg:"speedtest_result_id,pk,type:uuid,default:uuid_generate_v4()"`
	SourceIP          string    `pg:"source_ip,notnull"`
	DestIP            string    `pg:"dest_ip,notnull"`
	SpeedMbps         float32   `pg:"speed_mbps,notnull"`
	RequestURL        string    `pg:"request_url,notnull"`
	DestType          string    `pg:"dest_type,notnull"`
	CreatedAt         time.Time `pg:"created_at,notnull,default:now()"`
	UpdatedAt         time.Time `pg:"updated_at,notnull,default:now()"`
}

func CreateSpeedtestResult(ctx context.Context, db pg.DBI, r *SpeedtestResult) error {
	_, err := db.Model(r).Context(ctx).Insert()
	if err != nil {
		return errors.Wrap(err, "failed to create speedtest result")
	}
	return nil
}

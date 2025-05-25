package kinopoisk_unofficial

import (
	"context"
	"github.com/go-pg/pg/v10"
	"github.com/go-pg/pg/v10/orm"
	"github.com/pkg/errors"
	"strings"
	"time"

	"github.com/satori/go.uuid"
)

type Query struct {
	tableName struct{} `pg:"kinopoisk_unofficial.query"`

	QueryID   uuid.UUID `pg:"query_id,pk,type:uuid,default:uuid_generate_v4()"`
	Title     string    `pg:"title"`
	Year      *int16    `pg:"year"`
	KpID      *int      `pg:"kp_id"`
	CreatedAt time.Time `pg:"created_at,default:now()"`
	UpdatedAt time.Time `pg:"updated_at,default:now()"`
}

func GetQuery(ctx context.Context, db *pg.DB, title string, year *int16) (*Query, error) {
	// Normalize title: trim and lowercase
	normalizedTitle := strings.ToLower(strings.TrimSpace(title))

	query := &Query{}

	err := db.Model(query).
		Where("title = ?", normalizedTitle).
		Context(ctx).
		Apply(func(q *orm.Query) (*orm.Query, error) {
			if year != nil {
				q = q.Where("year = ?", *year)
			} else {
				q = q.Where("year IS NULL")
			}
			return q, nil
		}).
		Limit(1).
		Select()

	if errors.Is(err, pg.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return query, nil
}

func InsertQueryIgnoreConflict(ctx context.Context, db *pg.DB, title string, year *int16, kpID *int) (*Query, error) {

	q := &Query{
		Title: strings.ToLower(strings.TrimSpace(title)),
		Year:  year,
		KpID:  kpID,
	}

	_, err := db.Model(q).
		Context(ctx).
		OnConflict("DO NOTHING").
		Insert()

	if err != nil && !errors.Is(err, pg.ErrNoRows) {
		return nil, err
	}

	return q, nil
}

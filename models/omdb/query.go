package omdb

import (
	"context"
	"errors"
	"github.com/go-pg/pg/v10"
	"github.com/go-pg/pg/v10/orm"
	uuid "github.com/satori/go.uuid"
	"strings"
	"time"
)

type Query struct {
	tableName struct{} `pg:"omdb.query"`

	QueryID   uuid.UUID `pg:"query_id,pk,type:uuid,default:uuid_generate_v4()"`
	Title     string    `pg:"title,notnull"`
	Year      *int16    `pg:"year"` // nullable
	Type      OmdbType  `pg:"type,notnull"`
	ImdbID    *string   `pg:"imdb_id"` // nullable FK
	CreatedAt time.Time `pg:"created_at,default:now()"`
	UpdatedAt time.Time `pg:"updated_at,default:now()"`
}

func GetQuery(ctx context.Context, db *pg.DB, title string, year *int16, omdbType OmdbType) (*Query, error) {
	// Normalize title: trim and lowercase
	normalizedTitle := strings.ToLower(strings.TrimSpace(title))

	query := &Query{}

	err := db.Model(query).
		Where("title = ?", normalizedTitle).
		Where("type = ?", omdbType).
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

func InsertQueryIgnoreConflict(ctx context.Context, db *pg.DB, title string, year *int16, omdbType OmdbType, imdbID *string) (*Query, error) {

	q := &Query{
		Title:  strings.ToLower(strings.TrimSpace(title)),
		Year:   year,
		Type:   omdbType,
		ImdbID: imdbID,
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

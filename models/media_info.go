package models

import (
	"context"
	"fmt"
	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	"strings"
	"time"
)

type MediaInfo struct {
	tableName struct{} `pg:"media_info"`

	ResourceID string    `pg:"resource_id,pk"`
	Status     int16     `pg:"status,use_zero"`
	HasMovie   bool      `pg:"has_movie,use_zero"`
	HasSeries  bool      `pg:"has_series,use_zero"`
	MediaCount int16     `pg:"media_count,use_zero"`
	Error      *string   `pg:"error"`
	CreatedAt  time.Time `pg:"created_at,default:now()"`
	UpdatedAt  time.Time `pg:"updated_at,default:now()"`

	// Relations
	Movies     []*Movie   `pg:"rel:has-many,fk:resource_id"`
	SeriesList []*Series  `pg:"rel:has-many,fk:resource_id"`
	Episodes   []*Episode `pg:"rel:has-many,fk:resource_id"`
}

type MediaInfoStatus int16

const (
	MediaInfoStatusProcessing MediaInfoStatus = iota
	MediaInfoStatusDone
	MediaInfoStatusNoMedia
	MediaInfoStatusError
)

func TryInsertOrLockMediaInfo(ctx context.Context, db *pg.DB, resourceID string, expire time.Duration, force bool) (*MediaInfo, error) {
	// Attempt to insert a new media_info with "processing" status
	info := MediaInfo{
		ResourceID: resourceID,
		Status:     int16(MediaInfoStatusProcessing),
	}

	_, err := db.Model(&info).Context(ctx).Insert()
	if err == nil {
		// Insert successful — this worker owns the processing
		return &info, nil
	}

	// If it's not a duplicate error — return immediately
	if !strings.Contains(err.Error(), "duplicate key") {
		return nil, err
	}

	if force {
		return &info, nil
	}

	// Record already exists — attempt to lock it
	tx, err := db.BeginContext(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Close() }()

	var existing MediaInfo

	err = tx.Model(&existing).
		Context(ctx).
		Where("resource_id = ?", resourceID).
		Where("status NOT IN (?)", pg.In([]int16{
			int16(MediaInfoStatusProcessing),
			int16(MediaInfoStatusNoMedia),
			int16(MediaInfoStatusError),
		})).
		Where("updated_at < now() - INTERVAL ?", fmt.Sprintf("%d seconds", int(expire.Seconds()))).
		For("UPDATE SKIP LOCKED").
		Limit(1).
		Select()

	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			// Lock failed or record is too fresh — skip processing
			return nil, nil
		}
		return nil, err
	}

	// Update status to "processing"
	existing.Status = int16(MediaInfoStatusProcessing)

	_, err = tx.Model(&existing).
		Context(ctx).
		Column("status").
		WherePK().
		Update()
	if err != nil {
		return nil, err
	}

	return &existing, tx.Commit()
}

func UpdateMediaInfo(ctx context.Context, db *pg.DB, info *MediaInfo) error {
	_, err := db.Model(info).
		Context(ctx).
		Column("status", "has_movie", "has_series", "media_count").
		WherePK().
		Update()
	return err
}

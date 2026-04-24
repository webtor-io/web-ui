package models

import (
	"context"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
)

// UserSubtitle is a per-user binding of a subtitle blob (stored in S3 under
// its SHA-256 hash) to a specific file inside a torrent.
type UserSubtitle struct {
	tableName struct{} `pg:"user_subtitle"`

	UserSubtitleID uuid.UUID `pg:"user_subtitle_id,pk"`
	UserID         uuid.UUID `pg:"user_id"`
	ResourceID     string    `pg:"resource_id"`
	Path           string    `pg:"path"`
	Hash           string    `pg:"hash"`
	OriginalName   string    `pg:"original_name"`
	Format         string    `pg:"format"`
	Size           int64     `pg:"size"`
	CreatedAt      time.Time `pg:"created_at"`
	UpdatedAt      time.Time `pg:"updated_at"`
}

// CreateUserSubtitle inserts a new binding. The caller is responsible for
// making sure the blob with this hash is already in S3.
func CreateUserSubtitle(ctx context.Context, db *pg.DB, us *UserSubtitle) error {
	_, err := db.Model(us).Context(ctx).Insert()
	if err != nil {
		return errors.Wrap(err, "failed to insert user_subtitle")
	}
	return nil
}

// ListUserSubtitlesForFile returns subtitles uploaded by a specific user for a
// single file inside a torrent, ordered oldest first.
func ListUserSubtitlesForFile(ctx context.Context, db *pg.DB, userID uuid.UUID, resourceID, path string) ([]*UserSubtitle, error) {
	var list []*UserSubtitle
	err := db.Model(&list).
		Context(ctx).
		Where("user_id = ? AND resource_id = ? AND path = ?", userID, resourceID, path).
		OrderExpr("created_at ASC").
		Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to list user subtitles")
	}
	return list, nil
}

// GetUserSubtitle loads a single row by id scoped to a user (used by the
// delete handler to both load the hash and enforce ownership).
func GetUserSubtitle(ctx context.Context, db *pg.DB, userID, id uuid.UUID) (*UserSubtitle, error) {
	var us UserSubtitle
	err := db.Model(&us).
		Context(ctx).
		Where("user_subtitle_id = ? AND user_id = ?", id, userID).
		Limit(1).
		Select()
	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to get user subtitle")
	}
	return &us, nil
}

// GetUserSubtitleByHash returns the row for a specific (user, hash), used when
// the file endpoint wants to verify the requester owns a copy of the blob.
// Currently the file endpoint is public (we proxy via torrent-http-proxy so
// SRT → VTT works through ~vtt/), but this helper is still useful for future
// access checks.
func GetUserSubtitleByHash(ctx context.Context, db *pg.DB, hash string) (*UserSubtitle, error) {
	var us UserSubtitle
	err := db.Model(&us).
		Context(ctx).
		Where("hash = ?", hash).
		Limit(1).
		Select()
	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to get user subtitle by hash")
	}
	return &us, nil
}

// ListUserSubtitleHashesForResource returns the hashes of every subtitle a
// user has uploaded for a given resource, across all files. Used by the
// stream-video job cache key: an upload or delete anywhere inside this
// torrent changes the hash set and invalidates the cached render so the
// freshly-uploaded subtitle actually appears as a <track> next time the
// user hits Play.
func ListUserSubtitleHashesForResource(ctx context.Context, db *pg.DB, userID uuid.UUID, resourceID string) ([]string, error) {
	var hashes []string
	err := db.Model((*UserSubtitle)(nil)).
		Context(ctx).
		ColumnExpr("hash").
		Where("user_id = ? AND resource_id = ?", userID, resourceID).
		OrderExpr("hash ASC").
		Select(&hashes)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list user subtitle hashes")
	}
	return hashes, nil
}

// CountUserSubtitlesForFile returns how many subtitles a user has already
// uploaded for one (resource_id, path). Enforces the per-file upload limit.
func CountUserSubtitlesForFile(ctx context.Context, db *pg.DB, userID uuid.UUID, resourceID, path string) (int, error) {
	count, err := db.Model((*UserSubtitle)(nil)).
		Context(ctx).
		Where("user_id = ? AND resource_id = ? AND path = ?", userID, resourceID, path).
		Count()
	if err != nil {
		return 0, errors.Wrap(err, "failed to count user subtitles")
	}
	return count, nil
}

// CountUserSubtitlesByHash returns how many bindings exist for a given hash.
// After deletion, a zero count means the S3 object is orphaned and should be
// removed. Callers must run this inside the same advisory-locked transaction
// as the DELETE to avoid a race with a concurrent upload of the same hash.
func CountUserSubtitlesByHash(ctx context.Context, tx *pg.Tx, hash string) (int, error) {
	count, err := tx.Model((*UserSubtitle)(nil)).
		Context(ctx).
		Where("hash = ?", hash).
		Count()
	if err != nil {
		return 0, errors.Wrap(err, "failed to count user subtitles by hash")
	}
	return count, nil
}

// DeleteUserSubtitleTx removes a row inside a caller-provided transaction and
// returns the hash of the deleted blob so the caller can decide whether to
// drop it from S3. Returns empty hash if the row did not exist or did not
// belong to the user.
func DeleteUserSubtitleTx(ctx context.Context, tx *pg.Tx, userID, id uuid.UUID) (string, error) {
	var us UserSubtitle
	_, err := tx.Model(&us).
		Context(ctx).
		Where("user_subtitle_id = ? AND user_id = ?", id, userID).
		Returning("hash").
		Delete()
	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return "", nil
		}
		return "", errors.Wrap(err, "failed to delete user subtitle")
	}
	return us.Hash, nil
}

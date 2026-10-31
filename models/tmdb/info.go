package tmdb

import (
	"context"
	"errors"
	"time"

	"github.com/go-pg/pg/v10"
)

type TmdbType int16

const (
	TmdbTypeMovie  TmdbType = 1
	TmdbTypeSeries TmdbType = 2
)

type Info struct {
	tableName struct{} `pg:"tmdb.info"`

	TmdbID    int            `pg:"tmdb_id,pk"`
	ImdbID    *string        `pg:"imdb_id"`
	Title     string         `pg:"title,notnull"`
	Year      *int16         `pg:"year"`
	Type      TmdbType       `pg:"type,notnull"`
	Metadata  map[string]any `pg:"metadata,type:jsonb"`
	CreatedAt time.Time      `pg:"created_at,default:now()"`
	UpdatedAt time.Time      `pg:"updated_at,default:now()"`
}

func GetInfoByID(ctx context.Context, db *pg.DB, tmdbID int) (*Info, error) {
	var info Info

	err := db.Model(&info).
		Context(ctx).
		Where("tmdb_id = ?", tmdbID).
		Limit(1).
		Select()

	if errors.Is(err, pg.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &info, nil
}

func UpsertInfo(ctx context.Context, db *pg.DB, tmdbID int, tmdbType TmdbType, metadata map[string]any) (*Info, error) {
	var title string
	if t, ok := metadata["title"].(string); ok && t != "" {
		title = t
	} else if n, ok := metadata["name"].(string); ok && n != "" {
		title = n
	}

	var yearPtr *int16
	if rd, ok := metadata["release_date"].(string); ok && len(rd) >= 4 {
		y := parseYear(rd)
		if y != 0 {
			yy := int16(y)
			yearPtr = &yy
		}
	} else if fad, ok := metadata["first_air_date"].(string); ok && len(fad) >= 4 {
		y := parseYear(fad)
		if y != 0 {
			yy := int16(y)
			yearPtr = &yy
		}
	}

	var imdbID *string
	if id, ok := metadata["imdb_id"].(string); ok && id != "" {
		imdbID = &id
	}

	m := &Info{
		TmdbID:   tmdbID,
		ImdbID:   imdbID,
		Title:    title,
		Year:     yearPtr,
		Type:     tmdbType,
		Metadata: metadata,
	}

	// imdb_id keeps the stored value when a re-fetch of the same title
	// carries none: TV details never do, and ensureByTmdbID adds it only when
	// the follow-up external_ids call succeeds, so a failed external_ids must
	// not erase a known id. Only for the same type: tmdb_id numbers films and
	// series independently, and a row taken over by the other type is another
	// title, whose IMDB id the old one is not.
	_, err := db.Model(m).
		Context(ctx).
		OnConflict("(tmdb_id) DO UPDATE").
		Set("type = EXCLUDED.type, metadata = EXCLUDED.metadata, title = EXCLUDED.title, year = EXCLUDED.year, imdb_id = CASE WHEN info.type = EXCLUDED.type THEN COALESCE(EXCLUDED.imdb_id, info.imdb_id) ELSE EXCLUDED.imdb_id END").
		Insert()

	return m, err
}

// ListWithoutCredits returns up to limit rows (tmdb_id and type only) last
// written before the given time whose metadata has no credits. Lowest ids
// first. The time bound goes first: the credits test detoasts the whole
// metadata of every row it reads (865 MB on prod), the updated_at one does
// not, and it leaves only the old rows to test.
func ListWithoutCredits(ctx context.Context, db *pg.DB, before time.Time, limit int) ([]Info, error) {
	var infos []Info
	err := db.ModelContext(ctx, &infos).
		Column("tmdb_id", "type").
		Where("updated_at < ?", before).
		Where("metadata->'credits' IS NULL").
		Order("tmdb_id").
		Limit(limit).
		Select()
	return infos, err
}

// MarkNoCredits records an empty credits block on a row TMDB no longer
// has details for (a deleted entry answers 404); the write also moves its
// updated_at past any cutoff, so ListWithoutCredits stops returning it.
func MarkNoCredits(ctx context.Context, db *pg.DB, tmdbID int) error {
	_, err := db.ExecContext(ctx,
		`UPDATE tmdb.info SET metadata = metadata || '{"credits": {}}'::jsonb WHERE tmdb_id = ?`, tmdbID)
	return err
}

// Genres is what the genres backfill reads off a tmdb.info row.
type Genres struct {
	TmdbID int      `pg:"tmdb_id"`
	ImdbID *string  `pg:"imdb_id"`
	Type   TmdbType `pg:"type"`
	Genres []any    `pg:"genres"` // metadata->'genres': [{"id": N, "name": "..."}]
}

// ListGenres returns the rows with any of these IMDB or TMDB ids, lowest
// tmdb_id first. Both ids are indexed, so only the matched rows' metadata is
// read, and of it only genres comes back.
func ListGenres(ctx context.Context, db *pg.DB, imdbIDs []string, tmdbIDs []int) ([]Genres, error) {
	var rows []Genres
	_, err := db.QueryContext(ctx, &rows, `
		SELECT tmdb_id, imdb_id, type, metadata->'genres' AS genres
		FROM tmdb.info
		WHERE imdb_id = ANY(?::text[]) OR tmdb_id = ANY(?::integer[])
		ORDER BY tmdb_id`,
		pg.Array(imdbIDs), pg.Array(tmdbIDs))
	return rows, err
}

// GetInfoByIMDBID looks up a cached TMDB record by its IMDB id. Used by the
// release-subscribe fake-door eligibility check to read `status` /
// `in_production` / `next_episode_to_air` off `metadata` without a TMDB API
// round-trip. Returns nil (no error) when the row isn't cached locally yet —
// in that case eligibility falls through to "not airing".
func GetInfoByIMDBID(ctx context.Context, db *pg.DB, imdbID string) (*Info, error) {
	if imdbID == "" {
		return nil, nil
	}
	var info Info
	err := db.Model(&info).
		Context(ctx).
		Where("imdb_id = ?", imdbID).
		Limit(1).
		Select()
	if errors.Is(err, pg.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// ListRecentPopular returns films released on or after minYear, sorted by
// TMDB vote_average descending. Used by the AI recommendations service to
// build the "recent releases" prompt block that compensates for Claude's
// training data cutoff. Only movies with an IMDB id are returned (the
// resolver drops non-IMDB entries anyway).
func ListRecentPopular(ctx context.Context, db *pg.DB, minYear int16, limit int) ([]Info, error) {
	var infos []Info
	err := db.ModelContext(ctx, &infos).
		Where("year >= ?", minYear).
		Where("type = ?", TmdbTypeMovie).
		Where("imdb_id IS NOT NULL").
		OrderExpr("(metadata->>'vote_average')::float DESC NULLS LAST").
		Limit(limit).
		Select()
	if err != nil {
		return nil, err
	}
	return infos, nil
}

func parseYear(dateStr string) int {
	if len(dateStr) < 4 {
		return 0
	}
	y := 0
	for i := 0; i < 4; i++ {
		c := dateStr[i]
		if c < '0' || c > '9' {
			return 0
		}
		y = y*10 + int(c-'0')
	}
	return y
}

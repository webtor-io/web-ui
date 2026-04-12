package enrich

import (
	"context"
	"strconv"
	"strings"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	tm "github.com/webtor-io/web-ui/models/tmdb"
	"github.com/webtor-io/web-ui/services/tmdb"
)

type TMDB struct {
	api *tmdb.Api
	pg  *cs.PG
}

func (s *TMDB) GetName() string {
	return "TMDB"
}

func NewTMDB(pg *cs.PG, api *tmdb.Api) *TMDB {
	if api == nil {
		return nil
	}
	return &TMDB{
		pg:  pg,
		api: api,
	}
}

func (s *TMDB) makeVideoMetadata(info *tm.Info) *models.VideoMetadata {
	var posterURL string
	if pp, ok := info.Metadata["poster_path"].(string); ok && pp != "" {
		posterURL = s.api.PosterURL(pp, "w500")
	}

	var plot string
	if ov, ok := info.Metadata["overview"].(string); ok {
		plot = ov
	}

	var rating *float64
	if va, ok := info.Metadata["vote_average"].(float64); ok && va > 0 {
		rating = &va
	}

	videoID := s.resolveVideoID(info)

	return &models.VideoMetadata{
		VideoID:   videoID,
		Title:     info.Title,
		Year:      info.Year,
		Plot:      plot,
		PosterURL: posterURL,
		Rating:    rating,
	}
}

func (s *TMDB) resolveVideoID(info *tm.Info) string {
	if info.ImdbID != nil && *info.ImdbID != "" {
		return *info.ImdbID
	}
	return "tmdb" + strconv.Itoa(info.TmdbID)
}

func (s *TMDB) Map(ctx context.Context, m *models.VideoContent, mt models.ContentType, force bool) (*models.VideoMetadata, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("db is nil")
	}

	ttype := tm.TmdbTypeMovie
	searchType := tmdb.TmdbTypeMovie
	if mt == models.ContentTypeSeries {
		ttype = tm.TmdbTypeSeries
		searchType = tmdb.TmdbTypeTV
	}

	q, err := tm.GetQuery(ctx, db, m.Title, m.Year, ttype)
	if err != nil {
		return nil, err
	}
	if q != nil && !force {
		if q.TmdbID == nil {
			return nil, nil
		}
		mi, err := tm.GetInfoByID(ctx, db, *q.TmdbID)
		if err != nil {
			return nil, err
		}
		if mi == nil {
			return nil, nil
		}
		return s.makeVideoMetadata(mi), nil
	}

	sr, err := s.api.Search(ctx, m.Title, m.Year, searchType)
	if err != nil {
		return nil, err
	}
	if sr == nil {
		log.Infof("no tmdb found for title %v and year %v", m.Title, m.Year)
		_, err = tm.InsertQueryIgnoreConflict(ctx, db, m.Title, m.Year, ttype, nil)
		if err != nil {
			return nil, err
		}
		return nil, nil
	}

	// Get full details
	details, err := s.api.GetDetails(ctx, sr.ID, searchType)
	if err != nil {
		return nil, err
	}
	if details == nil {
		_, err = tm.InsertQueryIgnoreConflict(ctx, db, m.Title, m.Year, ttype, nil)
		if err != nil {
			return nil, err
		}
		return nil, nil
	}

	// For TV shows, imdb_id is not in details — fetch via external_ids
	if _, ok := details["imdb_id"]; !ok {
		extIDs, err := s.api.GetExternalIDs(ctx, sr.ID, searchType)
		if err == nil && extIDs != nil {
			if imdbID, ok := extIDs["imdb_id"].(string); ok && imdbID != "" {
				details["imdb_id"] = imdbID
			}
		}
	}

	_, err = tm.InsertQueryIgnoreConflict(ctx, db, m.Title, m.Year, ttype, &sr.ID)
	if err != nil {
		return nil, err
	}

	info, err := tm.UpsertInfo(ctx, db, sr.ID, ttype, details)
	if err != nil {
		return nil, err
	}

	return s.makeVideoMetadata(info), nil
}

// GetTmdbID returns the TMDB ID for a given video ID by looking up the cache or calling the API.
// videoID can be an IMDB ID (tt*) or internal TMDB ID (tmdb*).
func (s *TMDB) GetTmdbID(ctx context.Context, videoID string, ct models.ContentType) (int, error) {
	// Handle our own tmdbXXX format
	if strings.HasPrefix(videoID, "tmdb") {
		id, err := strconv.Atoi(strings.TrimPrefix(videoID, "tmdb"))
		if err != nil {
			return 0, nil
		}
		return id, nil
	}

	db := s.pg.Get()
	if db == nil {
		return 0, errors.New("db is nil")
	}

	// Try to find in tmdb.info by imdb_id
	var info tm.Info
	err := db.Model(&info).
		Context(ctx).
		Where("imdb_id = ?", videoID).
		Limit(1).
		Select()
	if err == nil {
		return info.TmdbID, nil
	}

	// Not found in cache, call TMDB find API
	resp, err := s.api.FindByExternalID(ctx, videoID, "imdb_id")
	if err != nil {
		return 0, errors.Wrap(err, "find by external id")
	}

	if ct == models.ContentTypeSeries && len(resp.TVResults) > 0 {
		return resp.TVResults[0].ID, nil
	}
	if ct == models.ContentTypeMovie && len(resp.MovieResults) > 0 {
		return resp.MovieResults[0].ID, nil
	}

	return 0, nil
}

func (s *TMDB) MapByID(ctx context.Context, videoID string, ct models.ContentType, force bool) (*models.VideoMetadata, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("db is nil")
	}

	ttype := tm.TmdbTypeMovie
	searchType := tmdb.TmdbTypeMovie
	if ct == models.ContentTypeSeries {
		ttype = tm.TmdbTypeSeries
		searchType = tmdb.TmdbTypeTV
	}

	var tmdbID int

	if strings.HasPrefix(videoID, "tt") {
		var err error
		tmdbID, err = s.GetTmdbID(ctx, videoID, ct)
		if err != nil {
			return nil, err
		}
		if tmdbID == 0 {
			return nil, nil
		}
	} else if strings.HasPrefix(videoID, "tmdb") {
		id, err := strconv.Atoi(strings.TrimPrefix(videoID, "tmdb"))
		if err != nil {
			return nil, nil
		}
		tmdbID = id
	} else {
		return nil, nil
	}

	if !force {
		mi, err := tm.GetInfoByID(ctx, db, tmdbID)
		if err != nil {
			return nil, err
		}
		if mi != nil {
			return s.makeVideoMetadata(mi), nil
		}
	}

	info, err := s.ensureByTmdbID(ctx, db, tmdbID, ttype, searchType)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, nil
	}
	return s.makeVideoMetadata(info), nil
}

// ensureByTmdbID fetches full metadata from TMDB for a given tmdb_id and
// upserts it into tmdb.info. Shared by MapByID (on-demand per torrent)
// and RefreshPopular (batch cron). Returns nil when TMDB has no details
// for this id (rare, happens for deleted entries).
func (s *TMDB) ensureByTmdbID(ctx context.Context, db *pg.DB, tmdbID int, ttype tm.TmdbType, searchType tmdb.TmdbType) (*tm.Info, error) {
	details, err := s.api.GetDetails(ctx, tmdbID, searchType)
	if err != nil {
		return nil, err
	}
	if details == nil {
		return nil, nil
	}

	if _, ok := details["imdb_id"]; !ok {
		extIDs, err := s.api.GetExternalIDs(ctx, tmdbID, searchType)
		if err == nil && extIDs != nil {
			if imdbID, ok := extIDs["imdb_id"].(string); ok && imdbID != "" {
				details["imdb_id"] = imdbID
			}
		}
	}

	return tm.UpsertInfo(ctx, db, tmdbID, ttype, details)
}

// RefreshPopular implements PopularProvider. It calls TMDB discover to
// fetch popular recent movies and upserts each one into tmdb.info so the
// AI recommendations prompt can query them by year + rating.
//
// By default existing entries are skipped (idempotent, fast). When force
// is true, every discovered film is re-fetched from TMDB and upserted —
// useful after adding new fields to GetDetails (e.g. credits).
func (s *TMDB) RefreshPopular(ctx context.Context, releaseDateGte string, limit int, force bool) (int, error) {
	db := s.pg.Get()
	if db == nil {
		return 0, errors.New("db is nil")
	}

	added := 0
	page := 1
	seen := 0
	for seen < limit {
		results, totalPages, err := s.api.DiscoverMovies(ctx, releaseDateGte, 50, page)
		if err != nil {
			return added, errors.Wrapf(err, "discover page %d", page)
		}
		if len(results) == 0 {
			break
		}
		for _, r := range results {
			if seen >= limit {
				break
			}
			seen++

			if !force {
				existing, err := tm.GetInfoByID(ctx, db, r.ID)
				if err != nil {
					log.WithError(err).WithField("tmdb_id", r.ID).Warn("popular: db check failed")
					continue
				}
				if existing != nil {
					continue
				}
			}

			_, err = s.ensureByTmdbID(ctx, db, r.ID, tm.TmdbTypeMovie, tmdb.TmdbTypeMovie)
			if err != nil {
				log.WithError(err).WithField("tmdb_id", r.ID).WithField("title", r.Title).Warn("popular: enrich failed")
				continue
			}
			added++
		}
		if page >= totalPages {
			break
		}
		page++
	}
	return added, nil
}

var _ MetadataMapper = (*TMDB)(nil)
var _ DirectMapper = (*TMDB)(nil)
var _ PopularProvider = (*TMDB)(nil)

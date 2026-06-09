package enrich

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/lazymap"
	"github.com/webtor-io/web-ui/models"
	tm "github.com/webtor-io/web-ui/models/tmdb"
	"github.com/webtor-io/web-ui/services/tmdb"
)

type localizedText struct {
	Title string
	Plot  string
}

type TMDB struct {
	api      *tmdb.Api
	pg       *cs.PG
	locCache *lazymap.LazyMap[*localizedText]
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
		locCache: lazymap.New[*localizedText](&lazymap.Config{
			Expire:      10 * time.Minute,
			ErrorExpire: 30 * time.Second,
		}),
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
	if sr == nil && m.Year != nil {
		// The year extracted from a torrent name is often noisy: a remaster
		// release year, a year buried in a codec tag, or the END of a
		// year-range that makes it past the parser. TMDB indexes shows
		// under their premiere year only, so a year-constrained miss with
		// a valid title is usually fixable by dropping the year filter.
		// We retry exactly once and let the result, if any, persist under
		// the original (title, year) cache key so future torrents with the
		// same wrong year resolve from cache.
		log.Infof("no tmdb match for title %v with year %v, retrying without year", m.Title, *m.Year)
		sr, err = s.api.Search(ctx, m.Title, nil, searchType)
		if err != nil {
			return nil, err
		}
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

// GetTmdbID returns the TMDB ID for a given video ID by looking up the
// cache or calling the API, together with the RESOLVED content type.
// videoID can be an IMDB ID (tt*) or internal TMDB ID (tmdb*). The ct
// argument is only a hint: an IMDB id maps to exactly one TMDB entity, so
// the cached row's type / the populated find-result array is authoritative
// — a wrong hint (e.g. a client-supplied catalog type) must not turn into
// a silent miss or a cross-namespace fetch.
func (s *TMDB) GetTmdbID(ctx context.Context, videoID string, ct models.ContentType) (int, models.ContentType, error) {
	// Handle our own tmdbXXX format. The numeric id alone can't disambiguate
	// the movie/tv namespaces, so the hint stands.
	if strings.HasPrefix(videoID, "tmdb") {
		id, err := strconv.Atoi(strings.TrimPrefix(videoID, "tmdb"))
		if err != nil {
			return 0, ct, nil
		}
		return id, ct, nil
	}

	db := s.pg.Get()
	if db == nil {
		return 0, ct, errors.New("db is nil")
	}

	// Try to find in tmdb.info by imdb_id
	var info tm.Info
	err := db.Model(&info).
		Context(ctx).
		Where("imdb_id = ?", videoID).
		Limit(1).
		Select()
	if err == nil {
		actual := models.ContentTypeMovie
		if info.Type == tm.TmdbTypeSeries {
			actual = models.ContentTypeSeries
		}
		return info.TmdbID, actual, nil
	}

	// Not found in cache, call TMDB find API. Prefer the hinted namespace,
	// but fall back to the other one — find by IMDB id returns the entity
	// in whichever array matches its real type.
	resp, err := s.api.FindByExternalID(ctx, videoID, "imdb_id")
	if err != nil {
		return 0, ct, errors.Wrap(err, "find by external id")
	}

	preferTV := ct == models.ContentTypeSeries
	if preferTV && len(resp.TVResults) > 0 || !preferTV && len(resp.MovieResults) == 0 && len(resp.TVResults) > 0 {
		return resp.TVResults[0].ID, models.ContentTypeSeries, nil
	}
	if len(resp.MovieResults) > 0 {
		return resp.MovieResults[0].ID, models.ContentTypeMovie, nil
	}

	return 0, ct, nil
}

func (s *TMDB) MapByID(ctx context.Context, videoID string, ct models.ContentType, force bool) (*models.VideoMetadata, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("db is nil")
	}

	var tmdbID int

	if strings.HasPrefix(videoID, "tt") {
		var err error
		// ct is replaced by the resolved type so ensureByTmdbID below never
		// fetches the wrong movie/tv namespace off a bad hint.
		tmdbID, ct, err = s.GetTmdbID(ctx, videoID, ct)
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

	ttype := tm.TmdbTypeMovie
	searchType := tmdb.TmdbTypeMovie
	if ct == models.ContentTypeSeries {
		ttype = tm.TmdbTypeSeries
		searchType = tmdb.TmdbTypeTV
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

// tmdbLang maps our 2-letter locale code to the TMDB language tag.
// Special case: our "pt" is PT-BR.
func tmdbLang(lang string) string {
	if lang == "pt" {
		return "pt-BR"
	}
	return lang
}

// Localize returns the localized title and plot for a video ID in the
// given language. Uses a 3-tier cache: in-memory lazymap → DB
// (tmdb.localized) → TMDB API.
func (s *TMDB) Localize(ctx context.Context, videoID string, lang string) (string, string, error) {
	tmdbID, tmdbType, err := s.resolveLocalizeIDs(ctx, videoID)
	if err != nil {
		return "", "", err
	}
	if tmdbID == 0 {
		return "", "", nil
	}

	tl := tmdbLang(lang)
	cacheKey := fmt.Sprintf("%d:%s", tmdbID, tl)

	text, err := s.locCache.Get(cacheKey, func() (*localizedText, error) {
		return s.localizeFromDBOrAPI(ctx, tmdbID, tmdbType, tl)
	})
	if err != nil {
		return "", "", err
	}
	if text == nil {
		return "", "", nil
	}
	return text.Title, text.Plot, nil
}

func (s *TMDB) resolveLocalizeIDs(ctx context.Context, videoID string) (int, tmdb.TmdbType, error) {
	if strings.HasPrefix(videoID, "tmdb") {
		id, err := strconv.Atoi(strings.TrimPrefix(videoID, "tmdb"))
		if err != nil {
			return 0, "", nil
		}
		db := s.pg.Get()
		if db != nil {
			info, _ := tm.GetInfoByID(ctx, db, id)
			if info != nil && info.Type == tm.TmdbTypeSeries {
				return id, tmdb.TmdbTypeTV, nil
			}
		}
		return id, tmdb.TmdbTypeMovie, nil
	}

	if strings.HasPrefix(videoID, "tt") {
		db := s.pg.Get()
		if db == nil {
			return 0, "", errors.New("db is nil")
		}
		var info tm.Info
		err := db.Model(&info).
			Context(ctx).
			Where("imdb_id = ?", videoID).
			Limit(1).
			Select()
		if err != nil {
			if errors.Is(err, pg.ErrNoRows) {
				return 0, "", nil
			}
			return 0, "", err
		}
		tt := tmdb.TmdbTypeMovie
		if info.Type == tm.TmdbTypeSeries {
			tt = tmdb.TmdbTypeTV
		}
		return info.TmdbID, tt, nil
	}

	return 0, "", nil
}

func (s *TMDB) localizeFromDBOrAPI(ctx context.Context, tmdbID int, tmdbType tmdb.TmdbType, lang string) (*localizedText, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("db is nil")
	}

	cached, err := tm.GetLocalized(ctx, db, tmdbID, lang)
	if err != nil {
		return nil, errors.Wrap(err, "localize: db lookup failed")
	}
	if cached != nil {
		return &localizedText{Title: cached.Title, Plot: cached.Plot}, nil
	}

	raw, err := s.api.GetLocalizedDetails(ctx, tmdbID, tmdbType, lang)
	if err != nil {
		return nil, errors.Wrap(err, "localize: api call failed")
	}
	if raw == nil {
		return nil, nil
	}

	var title string
	if t, ok := raw["title"].(string); ok && t != "" {
		title = t
	} else if n, ok := raw["name"].(string); ok && n != "" {
		title = n
	}

	var plot string
	if ov, ok := raw["overview"].(string); ok {
		plot = ov
	}

	if title == "" && plot == "" {
		return nil, nil
	}

	_, err = tm.UpsertLocalized(ctx, db, tmdbID, lang, title, plot)
	if err != nil {
		log.WithError(err).WithField("tmdb_id", tmdbID).WithField("lang", lang).Warn("localize: failed to persist cache")
	}

	return &localizedText{Title: title, Plot: plot}, nil
}

// IsAiring reads `status` / `in_production` off the locally cached TMDB
// metadata. No external API call — if the row isn't cached yet, we return
// false and the release-subscribe banner just doesn't render (safe
// default). Powered by the AiringChecker capability interface; called
// from Enricher.IsAiringSeries on resource page render.
func (s *TMDB) IsAiring(ctx context.Context, videoID string) (bool, error) {
	db := s.pg.Get()
	if db == nil {
		return false, nil
	}
	info, err := tm.GetInfoByIMDBID(ctx, db, videoID)
	if err != nil {
		return false, err
	}
	if info == nil || info.Metadata == nil {
		return false, nil
	}
	if v, ok := info.Metadata["status"].(string); ok && v == "Returning Series" {
		return true, nil
	}
	if v, ok := info.Metadata["in_production"].(bool); ok && v {
		return true, nil
	}
	return false, nil
}

var _ MetadataMapper = (*TMDB)(nil)
var _ DirectMapper = (*TMDB)(nil)
var _ PopularProvider = (*TMDB)(nil)
var _ LocalizableMapper = (*TMDB)(nil)
var _ AiringChecker = (*TMDB)(nil)

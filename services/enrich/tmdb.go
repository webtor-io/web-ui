package enrich

import (
	"context"
	"fmt"
	"regexp"
	"slices"
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
	"github.com/webtor-io/web-ui/services/genre"
	"github.com/webtor-io/web-ui/services/tmdb"
)

type localizedText struct {
	Title string
	Plot  string
}

// maxReviews caps the number of reviews kept per title; maxReviewRunes
// truncates a single review body. TMDB reviews are occasionally
// multi-thousand-word essays — past ~2000 chars they stop being modal
// content, and the in-memory cache shouldn't hold them either.
const (
	maxReviews     = 5
	maxReviewRunes = 2000
)

// reviewLinkRe flags review bodies that carry links — on TMDB those are
// almost always self-promotion ("full review on my blog…") or outright
// spam, not content worth a modal slot. Matches explicit schemes and
// www.-prefixed hosts; bare domains are left alone (too many false
// positives on titles and abbreviations).
var reviewLinkRe = regexp.MustCompile(`(?i)\b(?:https?://|www\.)\S+`)

// reviewHasLink reports whether a review body should be dropped by the
// link filter.
func reviewHasLink(content string) bool {
	return reviewLinkRe.MatchString(content)
}

type TMDB struct {
	api      *tmdb.Api
	pg       *cs.PG
	locCache *lazymap.LazyMap[*localizedText]
	revCache *lazymap.LazyMap[[]Review]
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
		revCache: lazymap.New[[]Review](&lazymap.Config{
			Expire:      time.Hour,
			ErrorExpire: 30 * time.Second,
			// Reviews are the largest payloads cached in this file (up to
			// ~5×2000 runes per title) and keys arrive at modal-open rate
			// across all users — cap the map so an hour of busy browsing
			// can't grow it unbounded.
			Capacity: 1000,
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
		Genres:    genre.FromTMDB(info.Metadata["genres"]),
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
		// Re-validate cached matches against the current title guard.
		// Rows cached before the guard existed ("01" → "0187 UFO") must
		// stop resolving on a plain re-enrich, not only under --force.
		if !acceptableTMDBMatch(m.Title, &tmdb.SearchResult{Title: mi.Title}) {
			log.WithFields(log.Fields{
				"query":   m.Title,
				"result":  mi.Title,
				"tmdb_id": *q.TmdbID,
			}).Info("rejecting cached low-confidence tmdb title match")
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

	// TMDB /search returns substring / prefix fuzzy matches and we take
	// results[0]. Reject a result whose title doesn't actually resemble
	// the query — a weak query ("01") otherwise resolves to an arbitrary
	// obscure entry ("0187 UFO"). Cache the rejection as a miss (nil id)
	// so future torrents with the same title short-circuit here.
	// See services/enrich/title_match.go for the title-similarity rule.
	if !acceptableTMDBMatch(m.Title, sr) {
		log.WithFields(log.Fields{
			"query":   m.Title,
			"result":  sr.Title,
			"tmdb_id": sr.ID,
		}).Info("rejecting low-confidence tmdb title match")
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

// Reviews implements ReviewsProvider. Resolves the id against tmdb.info
// only (no API-side find call — same as Localize) and returns nil when
// the id isn't locally known, letting Enricher.ReviewsByID decide
// whether to pay a MapByID. A resolved id always yields a non-nil slice,
// empty when TMDB has no reviews, so the verdict is cacheable upstream.
// Reviews are served default-language (mostly English) — see
// docs/discover.md for why no locale filter is applied.
func (s *TMDB) Reviews(ctx context.Context, videoID string) ([]Review, error) {
	tmdbID, tmdbType, err := s.resolveLocalizeIDs(ctx, videoID)
	if err != nil {
		return nil, err
	}
	if tmdbID == 0 {
		return nil, nil
	}

	cacheKey := fmt.Sprintf("%d:%s", tmdbID, tmdbType)
	return s.revCache.Get(cacheKey, func() ([]Review, error) {
		raw, err := s.api.GetReviews(ctx, tmdbID, tmdbType)
		if err != nil {
			return nil, err
		}
		out := make([]Review, 0, maxReviews)
		for _, r := range raw {
			if reviewHasLink(r.Content) {
				continue
			}
			out = append(out, Review{
				Author:    r.Author,
				Rating:    r.Rating,
				Content:   truncateRunes(r.Content, maxReviewRunes),
				URL:       r.URL,
				CreatedAt: r.CreatedAt,
			})
			if len(out) == maxReviews {
				break
			}
		}
		return out, nil
	})
}

// truncateRunes cuts s to at most n runes, appending an ellipsis when
// something was dropped. Rune-based so multi-byte text doesn't get split
// mid-character.
func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

// Directors: enough to recognise a title, few enough for one line under
// the plot on a phone. Cast: what the photo strip holds; the collapsed line
// names the first four.
const (
	maxCreditDirectors = 2
	maxCreditCast      = 8
)

// Credits implements CreditsProvider off the cached tmdb.info row only, like
// IsAiring: no upstream call. Rows last written before GetDetails asked
// for credits (c3ef013e, in prod by ~2026-04-15) have none and answer empty.
func (s *TMDB) Credits(ctx context.Context, videoID string, ct models.ContentType) (*Credits, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("db is nil")
	}
	var info *tm.Info
	var err error
	switch {
	case strings.HasPrefix(videoID, "tt"):
		info, err = tm.GetInfoByIMDBID(ctx, db, videoID)
	case strings.HasPrefix(videoID, "tmdb"):
		id, aerr := strconv.Atoi(strings.TrimPrefix(videoID, "tmdb"))
		if aerr != nil {
			return nil, nil
		}
		info, err = tm.GetInfoByID(ctx, db, id)
	default:
		return nil, nil
	}
	if err != nil || info == nil {
		return nil, err
	}
	return creditsFor(info, videoID, ct), nil
}

// backfillMaxFailuresInARow ends a backfill run early: that many failed
// calls in a row means TMDB is down or rate-limiting, and the rest of the
// batch would only fail the same way.
const backfillMaxFailuresInARow = 10

// creditsInProdSince bounds the backfill: GetDetails asked for credits from
// c3ef013e, live in prod by 2026-04-15, so only rows last written before
// this can lack them for that reason.
var creditsInProdSince = time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC)

// backfillCallTimeout bounds one re-fetch: the cron's client has no timeout
// of its own, and one stalled response must not hold the run forever.
const backfillCallTimeout = 30 * time.Second

// BackfillCredits implements CreditsBackfiller: re-fetches up to limit
// cached rows written before GetDetails asked for credits. A row TMDB no
// longer has details for is marked so it is not asked again; a failed
// call leaves the row for the next run.
func (s *TMDB) BackfillCredits(ctx context.Context, limit int) (int, error) {
	db := s.pg.Get()
	if db == nil {
		return 0, errors.New("db is nil")
	}
	rows, err := tm.ListWithoutCredits(ctx, db, creditsInProdSince, limit)
	if err != nil {
		return 0, err
	}
	done, failed := 0, 0
	for _, r := range rows {
		searchType := tmdb.TmdbTypeMovie
		if r.Type == tm.TmdbTypeSeries {
			searchType = tmdb.TmdbTypeTV
		}
		cctx, cancel := context.WithTimeout(ctx, backfillCallTimeout)
		info, err := s.ensureByTmdbID(cctx, db, r.TmdbID, r.Type, searchType)
		cancel()
		if err != nil {
			log.WithError(err).WithField("tmdb_id", r.TmdbID).Warn("credits backfill: refetch failed")
			if failed++; failed == backfillMaxFailuresInARow {
				return done, errors.Errorf("%d failures in a row, stopping", failed)
			}
			continue
		}
		failed = 0
		if info == nil {
			if err := tm.MarkNoCredits(ctx, db, r.TmdbID); err != nil {
				return done, err
			}
		}
		done++
	}
	return done, nil
}

// genresBackfillBatch is how many metadata rows one round of BackfillGenres
// handles: one read of them, one indexed read of tmdb.info, one UPDATE.
const genresBackfillBatch = 500

// BackfillGenres implements GenresBackfiller: derives genres for up to limit
// movie_metadata / series_metadata rows that have none yet (NULL) from the
// cached tmdb.info rows. No TMDB call: a row with nothing cached gets '{}'.
func (s *TMDB) BackfillGenres(ctx context.Context, limit int) (int, error) {
	db := s.pg.Get()
	if db == nil {
		return 0, errors.New("db is nil")
	}
	done := 0
	for _, ct := range []models.ContentType{models.ContentTypeMovie, models.ContentTypeSeries} {
		for done < limit {
			rows, err := models.ListMetadataWithoutGenres(ctx, db, ct, min(genresBackfillBatch, limit-done))
			if err != nil {
				return done, err
			}
			if len(rows) == 0 {
				break
			}
			imdbIDs, tmdbIDs := []string{}, []int{}
			for _, r := range rows {
				if strings.HasPrefix(r.VideoID, "tt") {
					imdbIDs = append(imdbIDs, r.VideoID)
				} else if id, ok := tmdbNumber(r.VideoID); ok {
					tmdbIDs = append(tmdbIDs, id)
				}
			}
			infos, err := tm.ListGenres(ctx, db, imdbIDs, tmdbIDs)
			if err != nil {
				return done, err
			}
			n, err := models.SetMetadataGenres(ctx, db, ct, genresFor(rows, ct, infos))
			if err != nil {
				return done, err
			}
			if n == 0 {
				// Every row was filled by someone else meanwhile, or the
				// write did not take: either way, no progress to loop on.
				break
			}
			done += n
		}
	}
	return done, nil
}

// genresFor derives the genres of one content type's metadata rows from the
// tmdb.info rows ListGenres found for them (lowest tmdb_id first):
//   - tt…: the row with that IMDB id, preferring the table's type. imdb_id is
//     not unique in tmdb.info, but an IMDB id names one title, so a row of the
//     other type is still that title, the one MapByID would have stored.
//   - tmdbN: the row with that number, of the table's type only. TMDB numbers
//     films and series independently, so the other type is another title.
//   - kp…, anything else, or no row: none.
//
// Every row comes back with a non-nil slice, so none is written back as NULL.
func genresFor(rows []models.MetadataGenres, ct models.ContentType, infos []tm.Genres) []models.MetadataGenres {
	want := tm.TmdbTypeMovie
	if ct == models.ContentTypeSeries {
		want = tm.TmdbTypeSeries
	}
	byImdb := map[string]tm.Genres{}
	byTmdb := map[int]tm.Genres{}
	for _, in := range infos {
		if in.ImdbID != nil {
			if cur, ok := byImdb[*in.ImdbID]; !ok || cur.Type != want && in.Type == want {
				byImdb[*in.ImdbID] = in
			}
		}
		byTmdb[in.TmdbID] = in
	}
	out := make([]models.MetadataGenres, len(rows))
	for i, r := range rows {
		var in tm.Genres
		ok := false
		if strings.HasPrefix(r.VideoID, "tt") {
			in, ok = byImdb[r.VideoID]
		} else if id, isTmdb := tmdbNumber(r.VideoID); isTmdb {
			in, ok = byTmdb[id]
			ok = ok && in.Type == want
		}
		r.Genres = []string{}
		if ok {
			r.Genres = genre.FromTMDB(in.Genres)
		}
		out[i] = r
	}
	return out
}

// tmdbNumber reads the number off our own tmdbN video id.
func tmdbNumber(videoID string) (int, bool) {
	n, ok := strings.CutPrefix(videoID, "tmdb")
	if !ok {
		return 0, false
	}
	id, err := strconv.Atoi(n)
	return id, err == nil
}

// creditsFor answers for the row a video id resolved to. TMDB numbers films
// and series independently and tmdb.info keys on the number alone, so for a
// tmdbN id a row of the other type is another title: checked, nothing shown.
func creditsFor(info *tm.Info, videoID string, ct models.ContentType) *Credits {
	want := tm.TmdbTypeMovie
	if ct == models.ContentTypeSeries {
		want = tm.TmdbTypeSeries
	}
	if strings.HasPrefix(videoID, "tmdb") && info.Type != want {
		return &Credits{}
	}
	return creditsFromMetadata(info.Metadata)
}

// creditsFromMetadata reads the people line off a TMDB details response:
// crew with job "Director" (films, and about a quarter of series),
// created_by (series only) and the first cast entries in billing order. An adult title answers empty.
func creditsFromMetadata(md map[string]any) *Credits {
	c := &Credits{}
	if adult, _ := md["adult"].(bool); adult {
		return c
	}
	credits, _ := md["credits"].(map[string]any)
	c.Directors = names(creditPeople(credits["crew"], maxCreditDirectors, func(p map[string]any) bool {
		job, _ := p["job"].(string)
		return job == "Director"
	}))
	c.Creators = names(creditPeople(md["created_by"], maxCreditDirectors, nil))
	c.Cast = creditPeople(credits["cast"], maxCreditCast, nil)
	return c
}

// creditPeople collects up to max people with distinct names from a TMDB
// person array, optionally only the entries keep accepts. profile_path
// comes as "/abc.jpg"; the leading slash is dropped.
func creditPeople(v any, max int, keep func(map[string]any) bool) []CastMember {
	list, _ := v.([]any)
	var out []CastMember
	for _, e := range list {
		if len(out) == max {
			break
		}
		p, _ := e.(map[string]any)
		if keep != nil && !keep(p) {
			continue
		}
		name, _ := p["name"].(string)
		if name = strings.TrimSpace(name); name == "" || slices.ContainsFunc(out, func(m CastMember) bool { return m.Name == name }) {
			continue
		}
		profile, _ := p["profile_path"].(string)
		out = append(out, CastMember{Name: name, Profile: strings.TrimPrefix(profile, "/")})
	}
	return out
}

func names(people []CastMember) []string {
	var out []string
	for _, p := range people {
		out = append(out, p.Name)
	}
	return out
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

// IsSeasonAiring answers for one season off the same cached metadata as
// IsAiring: TMDB's `next_episode_to_air` / `last_episode_to_air` carry a
// season_number. No external call; an uncached row reads as "not airing".
func (s *TMDB) IsSeasonAiring(ctx context.Context, videoID string, season int) (bool, error) {
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
	return seasonAiring(info.Metadata, season), nil
}

// seasonAiring is the whole rule, pure so it can be tested against metadata
// shapes. A season is still producing episodes when TMDB's next episode to
// air belongs to it, or when the series is still in production and the
// season lies beyond the last episode that aired — an announced season
// nothing has aired for yet. A season at or before the last aired one with
// no next episode scheduled inside it is finished, whatever the series
// status says.
func seasonAiring(meta map[string]any, season int) bool {
	if season <= 0 {
		return false
	}
	if n, ok := episodeSeason(meta["next_episode_to_air"]); ok {
		return n == season
	}
	seriesAiring := false
	if v, ok := meta["status"].(string); ok && v == "Returning Series" {
		seriesAiring = true
	}
	if v, ok := meta["in_production"].(bool); ok && v {
		seriesAiring = true
	}
	if !seriesAiring {
		return false
	}
	last, ok := episodeSeason(meta["last_episode_to_air"])
	if !ok {
		// Still producing, but nothing tells us which season: keep the
		// series-level answer rather than hide every banner.
		return true
	}
	return season > last
}

// episodeSeason reads season_number off a TMDB episode stub (JSON numbers
// decode as float64; a nil or absent stub answers !ok).
func episodeSeason(v any) (int, bool) {
	ep, ok := v.(map[string]any)
	if !ok || ep == nil {
		return 0, false
	}
	switch n := ep["season_number"].(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	}
	return 0, false
}

var _ MetadataMapper = (*TMDB)(nil)
var _ DirectMapper = (*TMDB)(nil)
var _ PopularProvider = (*TMDB)(nil)
var _ LocalizableMapper = (*TMDB)(nil)
var _ AiringChecker = (*TMDB)(nil)
var _ SeasonAiringChecker = (*TMDB)(nil)
var _ ReviewsProvider = (*TMDB)(nil)
var _ CreditsProvider = (*TMDB)(nil)
var _ CreditsBackfiller = (*TMDB)(nil)
var _ GenresBackfiller = (*TMDB)(nil)

package enrich

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	services "github.com/webtor-io/common-services"
	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	ptn "github.com/webtor-io/web-ui/services/parse_torrent_name"
)

type Enricher struct {
	pg             *services.PG
	api            *api.Api
	mappers        []MetadataMapper
	episodeMappers []EpisodeMapper
	aiResolver     *AIResolver
}

type MetadataMapper interface {
	Map(ctx context.Context, query *models.VideoContent, getType models.ContentType, force bool) (*models.VideoMetadata, error)
	GetName() string
}

type DirectMapper interface {
	MapByID(ctx context.Context, videoID string, ct models.ContentType, force bool) (*models.VideoMetadata, error)
}

type EpisodeMapper interface {
	MapEpisodes(ctx context.Context, videoID string, season int, force bool) ([]*models.EpisodeMetadata, error)
	GetName() string
}

// PopularProvider is an optional capability of a MetadataMapper. Mappers
// that can fetch lists of currently popular / trending films implement
// this interface. Enricher.RefreshPopular iterates all mappers, calls
// RefreshPopular on those that support it, and each implementation
// upserts the results into its own cache tables.
type PopularProvider interface {
	RefreshPopular(ctx context.Context, releaseDateGte string, limit int, force bool) (int, error)
}

// LocalizableMapper is an optional capability of a MetadataMapper. Mappers
// that can return localized title/plot for a given language implement this
// interface. Today only TMDB supports it; OMDB is English-only, Kinopoisk
// could implement it for Russian in the future.
type LocalizableMapper interface {
	Localize(ctx context.Context, videoID string, lang string) (title string, plot string, err error)
}

func (s *Enricher) HasMappers() bool {
	return len(s.mappers) > 0
}

// RefreshPopular asks each metadata mapper that supports the
// PopularProvider interface to fetch and cache popular recent releases.
// Today only TMDB implements it; adding support in OMDB or Kinopoisk
// later requires zero changes here.
func (s *Enricher) RefreshPopular(ctx context.Context, releaseDateGte string, limit int, force bool) error {
	for _, m := range s.mappers {
		pp, ok := m.(PopularProvider)
		if !ok {
			continue
		}
		count, err := pp.RefreshPopular(ctx, releaseDateGte, limit, force)
		if err != nil {
			log.WithError(err).WithField("mapper", m.GetName()).Error("refresh popular failed")
			continue
		}
		log.WithFields(log.Fields{
			"mapper": m.GetName(),
			"count":  count,
			"force":  force,
		}).Info("refreshed popular films")
	}
	return nil
}

// Localize overlays localized title and plot onto md in-place.
// Skips if lang is "en" or md is nil. Iterates mappers that implement
// LocalizableMapper and uses the first successful result.
// Errors are logged but swallowed — English fallback is always safe.
func (s *Enricher) Localize(ctx context.Context, md *models.VideoMetadata, lang string) {
	if md == nil || lang == "en" || md.VideoID == "" {
		return
	}
	for _, m := range s.mappers {
		lm, ok := m.(LocalizableMapper)
		if !ok {
			continue
		}
		title, plot, err := lm.Localize(ctx, md.VideoID, lang)
		if err != nil {
			log.WithError(err).
				WithField("mapper", m.GetName()).
				WithField("video_id", md.VideoID).
				WithField("lang", lang).
				Debug("localize: mapper failed, trying next")
			continue
		}
		if title != "" {
			md.Title = title
		}
		if plot != "" {
			md.Plot = plot
		}
		return
	}
}

func NewEnricher(pg *services.PG, api *api.Api, mappers []MetadataMapper, episodeMappers []EpisodeMapper, aiResolver *AIResolver) *Enricher {
	return &Enricher{
		pg:             pg,
		api:            api,
		mappers:        mappers,
		episodeMappers: episodeMappers,
		aiResolver:     aiResolver,
	}
}

func StructToMap(s interface{}) (map[string]interface{}, error) {
	var result map[string]interface{}

	data, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

type TorrentInfo struct {
	*ptn.TorrentInfo
	*ra.ListItem
}

func MakeTorrentInfo(item *ra.ListItem) (*TorrentInfo, error) {
	ti, err := parseItem(item)
	if err != nil {
		return nil, err
	}

	return &TorrentInfo{
		TorrentInfo: ti,
		ListItem:    item,
	}, nil
}

func parseItem(item *ra.ListItem) (ti *ptn.TorrentInfo, err error) {
	ti = &ptn.TorrentInfo{}
	pathParts := strings.Split(item.PathStr, "/")
	for _, part := range pathParts {
		if part == "" {
			continue
		}
		ti, err = ptn.Parse(ti, part)
		if err != nil {
			return nil, err
		}
	}
	return ti, nil
}

func (s *Enricher) enrichMediaInfo(ctx context.Context, db *pg.DB, hash string, claims *api.Claims, force bool, hintVideoID string) (*models.MediaInfoMediaType, error) {

	items, err := s.retrieveTorrentItems(ctx, hash, claims)

	if err != nil {
		return nil, err
	}

	//series := map[string]*models.Series{}
	//var movies []*models.Movie
	var torrentInfos []*TorrentInfo

	for _, item := range items {
		if item.MediaFormat != ra.Video {
			continue
		}
		ti, err := MakeTorrentInfo(&item)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to make torrent info for hash %v", hash)
		}
		torrentInfos = append(torrentInfos, ti)
	}

	if len(torrentInfos) == 0 {
		log.Infof("no media info acquired for hash %s", hash)
		return nil, nil
	}

	log.Infof("got %v media items", len(torrentInfos))

	mt := s.getMediaType(torrentInfos)

	log.Infof("got media type %v for hash %v", mt, hash)

	var series *models.Series
	var movies []*models.Movie

	switch mt {
	case models.MediaInfoMediaTypeMovieSingle:
		movie, err := s.makeMovie(torrentInfos, hash)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to make movie for hash %s", hash)
		}
		if movie != nil {
			movies = append(movies, movie)
		}
	case models.MediaInfoMediaTypeMovieMultiple:
		movies, err = s.makeMovies(torrentInfos, hash)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to make movies for hash %s", hash)
		}
	default:
		series, err = s.makeSeriesWithEpisodes(torrentInfos, hash, mt)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to make series for hash %s", hash)
		}
	}

	err = models.ReplaceMoviesForResource(ctx, db, hash, movies)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to replace movie for hash %s", hash)
	}

	var seriesSlice []*models.Series
	if series != nil {
		seriesSlice = append(seriesSlice, series)
	}
	err = models.ReplaceSeriesForResource(ctx, db, hash, seriesSlice)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to replace series for hash %s", hash)
	}

	movies, err = models.GetMoviesByResourceID(ctx, db, hash)

	if err != nil {
		return nil, errors.Wrapf(err, "failed to get movies for hash %s", hash)
	}

	for _, m := range movies {
		moviePath := ""
		if m.Path != nil {
			moviePath = *m.Path
		}
		md, err := s.mapMetadata(ctx, m.VideoContent, m.GetContentType(), force, hintVideoID, moviePath)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to map metadata for movie %+v hash %s", m, hash)
		}
		if md == nil {
			log.Warnf("no metadata for %v", m.VideoContent)
			continue
		}
		log.Infof("processing movie metadata %+v", md)
		metadataID, err := models.UpsertMovieMetadata(ctx, db, md)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to upsert metadata for movie %+v hash %s", md, hash)
		}
		err = models.LinkMovieToMetadata(ctx, db, m.MovieID, metadataID)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to link movie %+v with metadata for hash %s", m, hash)
		}
	}

	seriesSlice, err = models.GetSeriesByResourceID(ctx, db, hash)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get series for hash %s", hash)
	}

	for _, ser := range seriesSlice {
		var md *models.VideoMetadata
		if mt != models.MediaInfoMediaTypeSeriesCompilation && mt != models.MediaInfoMediaTypeSeriesSplitScenes {
			seriesPath, perr := models.GetFirstEpisodePathForSeries(ctx, db, ser.SeriesID)
			if perr != nil {
				log.WithError(perr).Warnf("failed to load representative episode path for series %v", ser.SeriesID)
			}
			md, err = s.mapMetadata(ctx, ser.VideoContent, ser.GetContentType(), force, hintVideoID, seriesPath)
		}
		if err != nil {
			return nil, errors.Wrapf(err, "failed to map metadata for hash %s", hash)
		}
		if md == nil {
			log.Warnf("no metadata for %v", ser.VideoContent)
			continue
		}
		log.Infof("processing series %+v", md)
		metadataID, err := models.UpsertSeriesMetadata(ctx, db, md)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to upsert series metadata for hash %s", hash)
		}
		err = models.LinkSeriesToMetadata(ctx, db, ser.SeriesID, metadataID)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to link series %+v with metadata for hash %s", ser, hash)
		}

		// Enrich episodes with metadata
		if len(s.episodeMappers) > 0 {
			err = s.enrichEpisodes(ctx, db, ser, md.VideoID, force)
			if err != nil {
				log.WithError(err).Warnf("failed to enrich episodes for series %v hash %s", md.VideoID, hash)
			}
		}
	}
	return &mt, nil
}

// LookupByVideoID iterates through mappers that implement DirectMapper and
// returns the first match for the given video ID (typically an IMDB tt* id
// or our internal kp{id}). Used by the poster proxy when a film exists
// in tmdb.info / kpu.info (via AI/discover enrichment) but not yet in
// movie_metadata (which is populated through the torrent enrichment flow).
//
// A mapper-result without a poster URL is treated as a miss and the loop
// continues — otherwise a thin response (e.g. OMDB returning "Poster":"N/A"
// for a marginal id) would short-circuit the chain and prevent another
// mapper from supplying a usable poster.
func (s *Enricher) LookupByVideoID(ctx context.Context, videoID string, ct models.ContentType) (*models.VideoMetadata, error) {
	for _, m := range s.mappers {
		dm, ok := m.(DirectMapper)
		if !ok {
			continue
		}
		md, err := dm.MapByID(ctx, videoID, ct, false)
		if err != nil {
			log.WithError(err).WithField("mapper", m.GetName()).WithField("video_id", videoID).Warn("direct lookup failed")
			continue
		}
		if md != nil && md.PosterURL != "" {
			return md, nil
		}
	}
	return nil, nil
}

// LookupByTitleYear iterates through configured metadata mappers (TMDB, OMDB,
// Kinopoisk, ...) and returns the first matching video metadata for the given
// title and optional year.
//
// Unlike Enrich, this lookup does not persist anything against a torrent
// resource — it is intended for flows that only have text identifiers (e.g.
// AI recommendations, manual search). Individual mappers may still cache
// results in their own tables, which later benefits regular torrent
// enrichment.
//
// AI fallback is NOT triggered here: this path is for non-torrent
// identifiers and has no filename to feed Claude.
func (s *Enricher) LookupByTitleYear(ctx context.Context, title string, year *int16, ct models.ContentType) (*models.VideoMetadata, error) {
	vc := &models.VideoContent{
		Title: title,
		Year:  year,
	}
	return s.mapMetadata(ctx, vc, ct, false, "", "")
}

func (s *Enricher) Enrich(ctx context.Context, hash string, claims *api.Claims, force bool, hintVideoID string) error {
	log.Infof("enriching media info for hash %s", hash)
	if hintVideoID != "" {
		log.Infof("enrichment hint video id: %s", hintVideoID)
	}
	db := s.pg.Get()
	if db == nil {
		return errors.New("db is nil")
	}
	mi, err := models.TryInsertOrLockMediaInfo(ctx, db, hash, 24*time.Hour, force)
	if err != nil {
		return err
	}
	if mi == nil {
		log.Infof("no media info acquired for hash %s", hash)
		return nil
	}
	log.Infof("start processing media info %+v", mi)

	mt, err := s.enrichMediaInfo(ctx, db, hash, claims, force, hintVideoID)
	if err != nil {
		if strings.Contains(err.Error(), "PermissionDenied") {
			mi.Status = int16(models.MediaInfoStatusForbidden)
		} else {
			errStr := err.Error()
			mi.Error = &errStr
			mi.Status = int16(models.MediaInfoStatusError)
		}
		log.WithError(err).Error("failed to enrich media info")
	} else if mt == nil {
		mi.Status = int16(models.MediaInfoStatusNoMedia)
	} else {
		mi.Status = int16(models.MediaInfoStatusDone)
		mtInt16 := int16(*mt)
		mi.MediaType = &mtInt16
	}
	err = models.UpdateMediaInfo(ctx, db, mi)
	if err != nil {
		return err
	}

	return nil
}
func (s *Enricher) enrichEpisodes(ctx context.Context, db *pg.DB, ser *models.Series, videoID string, force bool) error {
	// Reload series with episodes
	serWithEps, err := models.GetSeriesWithEpisodes(ctx, db, ser.SeriesID)
	if err != nil {
		return errors.Wrap(err, "failed to get series with episodes")
	}

	// Collect unique seasons
	seasons := map[int]bool{}
	for _, ep := range serWithEps.Episodes {
		if ep.Season != nil {
			seasons[int(*ep.Season)] = true
		}
	}

	// Fetch episode metadata for each season
	seasonEpisodes := map[int][]*models.EpisodeMetadata{}
	for season := range seasons {
		eps, err := s.mapEpisodeMetadata(ctx, videoID, season, force)
		if err != nil {
			log.WithError(err).Warnf("failed to map episode metadata for season %d", season)
			continue
		}
		if eps != nil {
			seasonEpisodes[season] = eps
		}
	}

	// Link episode metadata to episodes
	for _, ep := range serWithEps.Episodes {
		if ep.Season == nil || ep.Episode == nil {
			continue
		}
		season := int(*ep.Season)
		episodeNum := int(*ep.Episode)

		eps, ok := seasonEpisodes[season]
		if !ok {
			continue
		}

		for _, emd := range eps {
			if int(emd.Season) == season && int(emd.Episode) == episodeNum {
				metadataID, err := models.UpsertEpisodeMetadata(ctx, db, emd)
				if err != nil {
					log.WithError(err).Warnf("failed to upsert episode metadata S%dE%d", season, episodeNum)
					continue
				}
				err = models.LinkEpisodeToMetadata(ctx, db, ep.EpisodeID, metadataID)
				if err != nil {
					log.WithError(err).Warnf("failed to link episode S%dE%d to metadata", season, episodeNum)
				}
				break
			}
		}
	}

	return nil
}

func (s *Enricher) mapEpisodeMetadata(ctx context.Context, videoID string, season int, force bool) ([]*models.EpisodeMetadata, error) {
	for _, m := range s.episodeMappers {
		eps, err := m.MapEpisodes(ctx, videoID, season, force)
		if err != nil {
			return nil, errors.Wrapf(err, "got \"%v\" episode mapper error", m.GetName())
		}
		if eps != nil {
			return eps, nil
		}
	}
	return nil, nil
}

// mapMetadata resolves metadata for a (title, year) pair through the
// configured providers in priority order, with two fallback paths.
//
// pathHint is the original torrent filename / folder path. When non-empty
// AND every provider misses, we hand it to the AI resolver — Claude
// normalizes transliterated / mangled release names into candidate
// (title, year) tuples that the same mappers can then resolve. The
// visible metadata still comes from a real provider; Claude only
// supplies search keys.
func (s *Enricher) mapMetadata(ctx context.Context, vc *models.VideoContent, t models.ContentType, f bool, hintVideoID string, pathHint string) (*models.VideoMetadata, error) {
	if hintVideoID != "" {
		if md := s.lookupByHint(ctx, hintVideoID, t, f); md != nil {
			return md, nil
		}
	}
	md, firstErr := s.searchAllMappers(ctx, vc, t, f)
	if md != nil {
		return md, nil
	}
	if s.aiResolver != nil && pathHint != "" {
		if aiMD := s.tryAIFallback(ctx, vc, t, f, pathHint); aiMD != nil {
			return aiMD, nil
		}
	}
	// Nothing resolved. If at least one mapper cleanly missed (no errors
	// anywhere), this is a real "no metadata" outcome — return nil and
	// let the caller mark the resource Done/NoMedia. Otherwise surface
	// the original error so the resource lands in Status=Error and
	// `enrich --force-error` can retry it once the upstream API recovers.
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, nil
}

// lookupByHint walks every DirectMapper for an externally-provided
// videoID hint (e.g. an imdbId carried over from AI Discover). Returns
// the first non-nil match or nil when no mapper recognizes the id.
func (s *Enricher) lookupByHint(ctx context.Context, hintVideoID string, t models.ContentType, f bool) *models.VideoMetadata {
	for _, m := range s.mappers {
		dm, ok := m.(DirectMapper)
		if !ok {
			continue
		}
		md, err := dm.MapByID(ctx, hintVideoID, t, f)
		if err != nil {
			log.WithError(err).Warnf("direct mapper %q failed for hint %v", m.GetName(), hintVideoID)
			continue
		}
		if md != nil {
			log.Infof("direct mapper %q resolved hint %v", m.GetName(), hintVideoID)
			return md
		}
	}
	log.Infof("no mapper handled hint %v, falling back to title+year", hintVideoID)
	return nil
}

// searchAllMappers walks every Map mapper for the given VideoContent.
// On the first hit it runs tryUpgrade and returns the (possibly
// upgraded) metadata with a nil error. On all-miss returns (nil, err)
// where err is the first mapper error encountered, or nil if every
// mapper cleanly missed.
//
// Per-mapper errors are absorbed instead of aborting the chain — a
// single transient failure on a higher-priority mapper (classic case:
// free OMDB key hitting its 1000/day rate limit) would otherwise mask
// every later mapper AND the AI fallback. The first error is surfaced
// only when every path ultimately fails so retry semantics
// (`enrich --force-error`) stay intact.
func (s *Enricher) searchAllMappers(ctx context.Context, vc *models.VideoContent, t models.ContentType, f bool) (*models.VideoMetadata, error) {
	var firstErr error
	for i, m := range s.mappers {
		md, err := m.Map(ctx, vc, t, f)
		if err != nil {
			log.WithError(err).WithField("mapper", m.GetName()).Warn("mapper failed, continuing to next")
			if firstErr == nil {
				firstErr = errors.Wrapf(err, "got \"%v\" mapper error", m.GetName())
			}
			continue
		}
		if md == nil {
			continue
		}
		// Try to upgrade through higher-priority mappers using the
		// videoID just resolved. The most common payoff is the
		// Kinopoisk-only-resolvable Russian-titled torrents whose
		// canonical imdbId TMDB knows by external_id even when its
		// Search-by-title misses. KPU (lowest priority) advertises its
		// claimed imdbId as the videoID exactly so this loop can run;
		// if the imdbId is bogus, MapByID type-filtering at TMDB and
		// OMDB rejects it and we keep the lower-priority result intact.
		if upgraded := s.tryUpgrade(ctx, md, t, i); upgraded != nil {
			return upgraded, nil
		}
		return md, nil
	}
	return nil, firstErr
}

// tryAIFallback asks Claude for candidate (title, year) tuples and runs
// each through the same mapper chain as the original parsed title.
// Returns the resolved metadata on the first hit, or nil when no
// candidate produces a match. AI errors are best-effort and swallowed
// inside the resolver.
//
// No caching layer — the per-mapper TMDB.query / KPU.query caches
// already absorb repeat searches across resources, and an AI-side
// cache would either survive `--force` (breaking the "user wants
// Claude to retry" semantic) or duplicate the existing
// media_info-status gate. See docs/ai_enrichment.md for the rationale.
func (s *Enricher) tryAIFallback(ctx context.Context, vc *models.VideoContent, t models.ContentType, f bool, pathHint string) *models.VideoMetadata {
	candidates := s.aiResolver.SuggestCandidates(ctx, pathHint, vc.Title, vc.Year, t, f)
	for _, cand := range candidates {
		candVC := &models.VideoContent{
			ResourceID: vc.ResourceID,
			Title:      cand.Title,
			Year:       cand.Year,
		}
		md, _ := s.searchAllMappers(ctx, candVC, t, f)
		if md != nil {
			log.WithFields(log.Fields{
				"candidate":   cand.Title,
				"resolved_id": md.VideoID,
			}).Info("ai_enrich: candidate resolved")
			return md
		}
	}
	return nil
}

// tryUpgrade walks the mappers ABOVE `current` (more authoritative ones)
// and returns the first MapByID hit with a usable poster. Each mapper's
// MapByID is expected to type-filter on its own — we do not enforce the
// content-type match at this layer.
func (s *Enricher) tryUpgrade(ctx context.Context, md *models.VideoMetadata, t models.ContentType, current int) *models.VideoMetadata {
	if md == nil || md.VideoID == "" {
		return nil
	}
	for j := 0; j < current; j++ {
		dm, ok := s.mappers[j].(DirectMapper)
		if !ok {
			continue
		}
		up, err := dm.MapByID(ctx, md.VideoID, t, false)
		if err != nil {
			log.WithError(err).WithField("mapper", s.mappers[j].GetName()).WithField("video_id", md.VideoID).Warn("upgrade lookup failed")
			continue
		}
		if up != nil && up.PosterURL != "" {
			log.Infof("upgraded metadata for %v via mapper %q", md.VideoID, s.mappers[j].GetName())
			return up
		}
	}
	return nil
}

func (s *Enricher) retrieveTorrentItems(ctx context.Context, hash string, claims *api.Claims) ([]ra.ListItem, error) {
	limit := uint(100)
	offset := uint(0)
	var items []ra.ListItem
	for {
		resp, err := s.api.ListResourceContentCached(ctx, claims, hash, &api.ListResourceContentArgs{
			Limit:  limit,
			Offset: offset,
		})
		if err != nil {
			return nil, err
		}
		for _, item := range resp.Items {
			items = append(items, item)
		}
		if (resp.Count - int(offset)) == len(resp.Items) {
			break
		}
		offset += limit
	}
	return items, nil
}

func (s *Enricher) getMediaType(infos []*TorrentInfo) models.MediaInfoMediaType {
	var hasSeasones, hasDifferentSeasones, hasEpisodes, sameTitle, hasScenes bool
	sameTitle = true
	var title string
	var season int
	for _, info := range infos {
		if info.Season != 0 && season != info.Season && season != 0 {
			hasDifferentSeasones = true
		}
		if info.Season != 0 {
			season = info.Season
			hasSeasones = true
		}
		if info.Episode != 0 {
			hasEpisodes = true
		}
		if info.Scene != 0 {
			hasScenes = true
		}
		if title != "" && info.Title != title {
			sameTitle = false
		}
		title = info.Title
	}
	// MovieSingle on small torrents without ANY structural series markers
	// (no season, no scene). A parser-extracted "episode" alone is not
	// trustworthy — single-digit "Movie - 1.mkv" pack titles, multi-CD
	// movies, and odd codec tags can all leak an episode number out of a
	// movie filename. With <3 files and a consistent title, treating it as
	// a movie recovers far more cases than it breaks.
	if len(infos) < 3 && sameTitle && !hasScenes && !hasSeasones {
		return models.MediaInfoMediaTypeMovieSingle
	}
	if hasSeasones && hasEpisodes && hasDifferentSeasones {
		return models.MediaInfoMediaTypeSeriesMultipleSeasons
	}
	// SeriesSingleSeason fires when EITHER an explicit season tag is
	// present, OR the pack looks like a season-less anime/fansub release
	// (consistent title across >=3 files, all parsed as episodes). This
	// blocks Le-Hobbit-style multi-movie compilations (sameTitle=false
	// because each movie has a different title) from being mislabelled as
	// a series and falling through to KPU.
	hasSequentialEpisodes := sameTitle && len(infos) >= 3 && hasEpisodes && !hasDifferentSeasones
	if (hasSeasones || hasSequentialEpisodes) && hasEpisodes && !hasDifferentSeasones {
		return models.MediaInfoMediaTypeSeriesSingleSeason
	}
	if hasScenes && !hasDifferentSeasones {
		return models.MediaInfoMediaTypeSeriesSplitScenes
	}
	// MovieMultiple — distinct titles across files with no season/scene
	// markers. Typical case: a movie-trilogy or franchise pack (e.g.
	// "Home Alone 1/2/3", "Le Hobbit + LOTR"). Each file is its own
	// movie; downstream we group by parsed title so a duplicated title
	// (multi-disc / multi-audio of the same film) collapses to one row.
	if !sameTitle && !hasSeasones && !hasScenes {
		return models.MediaInfoMediaTypeMovieMultiple
	}
	return models.MediaInfoMediaTypeSeriesCompilation
}

func (s *Enricher) makeMovie(infos []*TorrentInfo, hash string) (*models.Movie, error) {
	ti := infos[0]
	movie := &models.Movie{
		VideoContent: &models.VideoContent{
			ResourceID: hash,
		},
	}
	movie.Title = ti.Title
	if ti.Year != 0 {
		year := int16(ti.Year)
		movie.Year = &year
	}
	movie.Path = &ti.PathStr
	metadata, err := StructToMap(ti.TorrentInfo)
	if err != nil {
		return nil, err
	}
	movie.Metadata = metadata
	return movie, nil
}

// makeMovies builds one Movie per distinct parsed title from a multi-movie
// torrent pack. Multi-disc / multi-audio variants of the same film share
// a parsed title and collapse into a single Movie (the first occurrence
// wins for path/metadata). The output preserves the input order of first
// occurrences so subsequent enrichment is deterministic.
func (s *Enricher) makeMovies(infos []*TorrentInfo, hash string) ([]*models.Movie, error) {
	seen := make(map[string]bool, len(infos))
	var movies []*models.Movie
	for _, ti := range infos {
		key := ti.Title
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		movie, err := s.makeMovie([]*TorrentInfo{ti}, hash)
		if err != nil {
			return nil, err
		}
		movies = append(movies, movie)
	}
	return movies, nil
}

func (s *Enricher) makeSeriesWithEpisodes(infos []*TorrentInfo, hash string, mt models.MediaInfoMediaType) (*models.Series, error) {
	var ti *TorrentInfo
	var err error
	if mt == models.MediaInfoMediaTypeSeriesCompilation || mt == models.MediaInfoMediaTypeSeriesSplitScenes {
		ti, err = s.makeCompilationTorrentInfo(infos)
	} else {
		ti, err = s.makeStandardSeriesTorrentInfo(infos)
	}
	if err != nil {
		return nil, err
	}
	ser := &models.Series{
		VideoContent: &models.VideoContent{
			ResourceID: hash,
		},
		SeriesID: uuid.NewV4(),
	}
	title := ti.Title
	if ti.Title == "" {
		title = ti.Website
	}
	ser.Title = title
	if ti.Year != 0 {
		year := int16(ti.Year)
		ser.Year = &year
	}
	for _, ti = range infos {
		episode := ti.Episode
		if episode == 0 {
			episode = ti.Scene
		}
		var sea *int16
		if ti.Season != 0 {
			seaInt16 := int16(ti.Season)
			sea = &seaInt16
		}
		var ep *int16
		if episode != 0 {
			epInt16 := int16(episode)
			ep = &epInt16
		}
		e := &models.Episode{
			ResourceID: hash,
			SeriesID:   ser.SeriesID,
			Path:       &ti.PathStr,
			Season:     sea,
			Episode:    ep,
		}
		metadata, err := StructToMap(ti.TorrentInfo)
		if err != nil {
			return nil, err
		}
		e.Metadata = metadata
		ser.Episodes = append(ser.Episodes, e)
	}
	return ser, nil
}

func (s *Enricher) makeCompilationTorrentInfo(infos []*TorrentInfo) (*TorrentInfo, error) {
	ti := &ptn.TorrentInfo{}
	pathParts := strings.Split(strings.TrimPrefix(infos[0].PathStr, "/"), "/")
	ti, err := ptn.Parse(ti, pathParts[0])
	if err != nil {
		return nil, err
	}
	return &TorrentInfo{
		ListItem:    infos[0].ListItem,
		TorrentInfo: ti,
	}, nil
}

func (s *Enricher) makeStandardSeriesTorrentInfo(infos []*TorrentInfo) (*TorrentInfo, error) {
	for _, info := range infos {
		if info.Episode != 0 {
			return info, nil
		}
	}
	return nil, errors.New("no episode info in torrent list")
}

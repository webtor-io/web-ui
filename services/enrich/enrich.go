package enrich

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
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

// Review is a single external user review surfaced through the
// ReviewsProvider capability. Rating is the author's 0-10 score when
// present; CreatedAt is an RFC3339 timestamp string straight from the
// upstream API.
type Review struct {
	Author    string
	Rating    *float64
	Content   string
	URL       string
	CreatedAt string
}

// ReviewsProvider is an optional capability of a MetadataMapper. Mappers
// that can fetch user reviews for a video id implement it. Today only
// TMDB does. Contract mirrors LocalizeByID's three-valued shape: a
// non-nil (possibly empty) slice means "checked, this is what exists";
// nil with nil error means the mapper could not resolve the id locally
// (the Enricher may MapByID and retry); non-nil error means the check
// itself failed and the caller must not cache the miss.
type ReviewsProvider interface {
	Reviews(ctx context.Context, videoID string) ([]Review, error)
}

// AiringChecker is an optional capability of a MetadataMapper. Mappers that
// know "is this series currently producing new episodes" implement it.
// Today only TMDB does — it reads `status` and `in_production` off its
// cached metadata. Adding KPU/anidb support later is one method.
type AiringChecker interface {
	IsAiring(ctx context.Context, videoID string) (bool, error)
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

// walkByID implements the shared by-id capability walk used by
// LocalizeByID and ReviewsByID: for each mapper, `try` reports whether
// the mapper supports the capability and whether it produced a result
// (writing the result into variables the caller captures). On a
// resolve-miss (supported, no result, no error) the mapper gets exactly
// one DirectMapper.MapByID call to pull a previously unseen id into its
// local cache, followed by one retry — so already-known ids never pay
// the extra resolution queries.
//
// Returns nil as soon as any try reports found; otherwise returns the
// last error seen (nil when every mapper cleanly missed) — the same
// "checked, nothing exists" vs "could not check" distinction both
// callers expose to their negative-result-caching clients.
func (s *Enricher) walkByID(ctx context.Context, videoID string, ct models.ContentType, op string,
	try func(m MetadataMapper) (supported bool, found bool, err error)) error {
	var lastErr error
	for _, m := range s.mappers {
		supported, found, err := try(m)
		if !supported {
			continue
		}
		if err != nil {
			log.WithError(err).
				WithField("mapper", m.GetName()).
				WithField("video_id", videoID).
				Debugf("%s: mapper failed, trying next", op)
			lastErr = err
			continue
		}
		if found {
			return nil
		}

		dm, ok := m.(DirectMapper)
		if !ok {
			continue
		}
		md, err := dm.MapByID(ctx, videoID, ct, false)
		if err != nil {
			log.WithError(err).
				WithField("mapper", m.GetName()).
				WithField("video_id", videoID).
				Debugf("%s: map by id failed, trying next", op)
			lastErr = err
			continue
		}
		if md == nil {
			continue
		}
		_, found, err = try(m)
		if err != nil {
			lastErr = err
			continue
		}
		if found {
			return nil
		}
	}
	return lastErr
}

// LocalizeByID returns the localized title and plot for a bare video ID
// (IMDB tt* / tmdb*) without a pre-built VideoMetadata — the Discover
// catalog grid only has Stremio ids on hand. Walks the same mapper chain
// as Localize via walkByID: Localize is tried first (cheap:
// cache-backed); only on a resolve-miss does MapByID run once, followed
// by a retry.
//
// The returned error distinguishes "checked, no translation exists"
// (empty strings, nil error) from "could not check" (empty strings,
// non-nil error) — callers that cache negative results must not pin a
// transient failure as a permanent miss.
func (s *Enricher) LocalizeByID(ctx context.Context, videoID string, ct models.ContentType, lang string) (string, string, error) {
	if videoID == "" || lang == "en" {
		return "", "", nil
	}
	var title, plot string
	err := s.walkByID(ctx, videoID, ct, "localize by id", func(m MetadataMapper) (bool, bool, error) {
		lm, ok := m.(LocalizableMapper)
		if !ok {
			return false, false, nil
		}
		t, p, err := lm.Localize(ctx, videoID, lang)
		if err != nil {
			return true, false, err
		}
		if t != "" || p != "" {
			title, plot = t, p
			return true, true, nil
		}
		return true, false, nil
	})
	if title != "" || plot != "" {
		return title, plot, nil
	}
	return "", "", err
}

// ReviewsByID returns user reviews for a bare video ID (IMDB tt* /
// tmdb*). Same walkByID semantics as LocalizeByID: the cheap cached
// Reviews call goes first; only when the mapper can't resolve the id at
// all (nil, nil) does MapByID run once, followed by a retry. A resolved
// id with zero reviews comes back as an empty non-nil slice and is a
// definitive answer — no extra resolution work is spent on it.
//
// Same error contract as LocalizeByID: (nil, non-nil) means "could not
// check" and callers must not cache it as a permanent miss.
func (s *Enricher) ReviewsByID(ctx context.Context, videoID string, ct models.ContentType) ([]Review, error) {
	if videoID == "" {
		return nil, nil
	}
	var out []Review
	err := s.walkByID(ctx, videoID, ct, "reviews by id", func(m MetadataMapper) (bool, bool, error) {
		rp, ok := m.(ReviewsProvider)
		if !ok {
			return false, false, nil
		}
		revs, err := rp.Reviews(ctx, videoID)
		if err != nil {
			return true, false, err
		}
		if revs != nil {
			out = revs
			return true, true, nil
		}
		return true, false, nil
	})
	if out != nil {
		return out, nil
	}
	return nil, err
}

// IsAiringSeries asks any mapper that supports AiringChecker whether the
// series identified by videoID is currently airing. Returns true on the
// first positive hit; mapper errors are logged and swallowed (a wrong
// "false" just hides the release-subscribe banner, which is the safe
// default for the experiment).
func (s *Enricher) IsAiringSeries(ctx context.Context, videoID string) bool {
	if videoID == "" {
		return false
	}
	for _, m := range s.mappers {
		ac, ok := m.(AiringChecker)
		if !ok {
			continue
		}
		airing, err := ac.IsAiring(ctx, videoID)
		if err != nil {
			log.WithError(err).
				WithField("mapper", m.GetName()).
				WithField("video_id", videoID).
				Debug("airing check: mapper failed, trying next")
			continue
		}
		if airing {
			return true
		}
	}
	return false
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

// parseItem walks each non-empty segment of the torrent's file path
// independently, then merges the per-segment TorrentInfo structs.
//
// Strategy:
//
//  1. Take the FILE segment (last non-empty part) as the base — the
//     filename normally carries the most fields (Episode, Codec,
//     Quality, Year, Container, ...).
//  2. Fill gaps from earlier segments — anything the file didn't set
//     gets the first non-zero value seen from root → file-1.
//  3. Series-shape override: when the file has an Episode but NO
//     Season, the file's Title is almost certainly an episode
//     subtitle ("Episode 18 - Discos and Dragons"), not the series
//     name. In that case adopt the ROOT segment's Title instead so
//     metadata lookup keys on the series. The S01E01-style file
//     form, where Season is set, leaves Title alone — the parser
//     already extracted the series name as Title.
//  4. PathTitles — every segment's non-empty parsed Title, deduped
//     and root-first. Downstream enrichment can iterate the list as
//     additional TMDB/OMDB/KPU search candidates before falling
//     through to the AI resolver.
//
// Test fixture: services/enrich/parse_item_test.go.
func parseItem(item *ra.ListItem) (*ptn.TorrentInfo, error) {
	pathParts := strings.Split(item.PathStr, "/")
	segments := make([]*ptn.TorrentInfo, 0, len(pathParts))
	for _, part := range pathParts {
		if part == "" {
			continue
		}
		seg, err := ptn.Parse(&ptn.TorrentInfo{}, part)
		if err != nil {
			return nil, err
		}
		segments = append(segments, seg)
	}
	if len(segments) == 0 {
		return &ptn.TorrentInfo{}, nil
	}
	merged := *segments[len(segments)-1] // copy file segment as base
	for i := 0; i < len(segments)-1; i++ {
		mergeKeepFirstNonZero(&merged, segments[i])
	}
	root := segments[0]
	file := segments[len(segments)-1]
	if len(segments) > 1 && file.Episode > 0 && file.Season == 0 &&
		root.Title != "" && root.Title != file.Title {
		merged.Title = root.Title
	}
	for _, seg := range segments {
		if seg.Title != "" && !slices.Contains(merged.PathTitles, seg.Title) {
			merged.PathTitles = append(merged.PathTitles, seg.Title)
		}
	}
	return &merged, nil
}

// mergeKeepFirstNonZero copies fields from `src` into `dst` only
// where `dst` is currently a zero value. Slice fields are left alone
// — they're managed explicitly by parseItem (PathTitles) and never
// merged across segments.
func mergeKeepFirstNonZero(dst, src *ptn.TorrentInfo) {
	dstV := reflect.ValueOf(dst).Elem()
	srcV := reflect.ValueOf(src).Elem()
	for i := 0; i < dstV.NumField(); i++ {
		df := dstV.Field(i)
		sf := srcV.Field(i)
		if df.Kind() == reflect.Slice {
			continue
		}
		if df.IsZero() {
			df.Set(sf)
		}
	}
}

// torrentRoot returns the first non-empty segment of a torrent file
// path — typically the torrent's root folder name. Used when feeding
// a series path to the AI enrichment fallback: a per-episode filename
// like "01 - first joke.mkv" carries no series title, whereas the
// parent folder ("Stand.Up.S13.Complete") usually does. Returns the
// path unchanged when there is no separator.
func torrentRoot(path string) string {
	for _, part := range strings.Split(path, "/") {
		if part != "" {
			return part
		}
	}
	return path
}

// maxUniqueAICalls is the hard upper bound on fresh Claude calls per
// enrichMediaInfo run. Above 5, the cost of N misclassified-as-distinct
// episodes (Dragon Ball pack: 153 movie rows → 153 Claude calls, all
// returning "Dragon Ball") exceeds any plausible benefit on a single
// torrent. Legit multi-work packs (trilogy / box set) where every file
// is a genuinely-distinct title needing AI normalization rarely exceed
// 3–4 entries in practice.
const maxUniqueAICalls = 5

// resourceAIBudget governs the AI fallback within a single
// enrichMediaInfo run. Multi-file packs (MovieMultiple with N distinct
// titles, or a mis-classified episode pack) used to fire one Claude
// call per file — burning N tokens on a torrent that is, in practice,
// uniformly unenrichable OR uniformly enrichable to the same answer
// (Dragon Ball Clássico 001…153 → 153× "Dragon Ball"). Three signals
// shut AI down for the remainder of the run:
//
//  1. failed — any single AI fallback returned no resolvable metadata.
//     Subsequent files skip AI entirely. (Original semantics; kept.)
//  2. locked — two consecutive Claude calls returned the SAME candidate
//     set. Strong evidence that the parser sees N "different" titles
//     for one logical work. Remaining files reuse the locked set
//     through the mapper chain without re-calling Claude. If a locked
//     run fails to resolve a file, that file gets one more fresh
//     Claude attempt (so genuinely-distinct trailing entries in a pack
//     still have a chance).
//  3. uniqueCalls >= maxUniqueAICalls — hard cap. Beyond 5 fresh
//     Claude calls per resource we always skip.
//
// Passed as a pointer through mapMetadata → tryAIFallback. nil is the
// "no budget tracking" signal used by non-torrent flows like
// LookupByTitleYear (which has no pathHint and thus never runs AI).
type resourceAIBudget struct {
	failed         bool
	uniqueCalls    int
	lastCandidates []TitleCandidate
	locked         []TitleCandidate
}

// available reports whether tryAIFallback is allowed to call Claude
// AGAIN. The locked-candidates path bypasses this check — see
// tryAIFallback for the order of fallbacks.
func (b *resourceAIBudget) available() bool {
	if b == nil {
		return true
	}
	if b.failed {
		return false
	}
	return b.uniqueCalls < maxUniqueAICalls
}

func (b *resourceAIBudget) markFailed() {
	if b == nil {
		return
	}
	b.failed = true
}

// recordCall is invoked after each fresh Claude call (regardless of
// outcome). It increments the call counter and, when the current
// response is identical to the previous one, locks the budget to that
// candidate set so the remaining files skip Claude.
func (b *resourceAIBudget) recordCall(cands []TitleCandidate) {
	if b == nil {
		return
	}
	b.uniqueCalls++
	if b.locked != nil {
		return
	}
	if b.lastCandidates != nil && candidatesEqual(b.lastCandidates, cands) {
		b.locked = cands
		return
	}
	b.lastCandidates = cands
}

// candidatesEqual compares two candidate slices by (title, year) — the
// only fields that drive downstream mapper lookups. Language is
// informational and intentionally ignored.
func candidatesEqual(a, b []TitleCandidate) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Title != b[i].Title {
			return false
		}
		ay, by := a[i].Year, b[i].Year
		switch {
		case ay == nil && by == nil:
		case ay == nil || by == nil:
			return false
		case *ay != *by:
			return false
		}
	}
	return true
}

// isAdultPath runs the torrent-name parser over each path segment and
// returns true as soon as any segment flags adult content. Each level
// is parsed independently so a clean episode filename under a studio
// folder (e.g. "Blacked/lana.mp4" or "JAV_uncensored/abp-123.mp4")
// still trips on the folder. Returns false on an empty path.
func isAdultPath(pathStr string) bool {
	return matchesPathFlag(pathStr, func(ti *ptn.TorrentInfo) bool {
		return ti.Adult
	})
}

// isSportPath returns true when any path segment carries a recognised
// sport league / event marker (NBA, NHL, WWE, AEW, Premier League,
// КХЛ, ...). Sports broadcasts have no metadata coverage in
// TMDB/OMDB/KPU and Claude has nothing to add, so the AI fallback
// skips them just like adult content does.
func isSportPath(pathStr string) bool {
	return matchesPathFlag(pathStr, func(ti *ptn.TorrentInfo) bool {
		return ti.Sport
	})
}

// isCoursePath returns true for pirated-course / e-learning torrents
// (Udemy, Coursera, FreeCourseSite, TutsNode, DevCourseWeb, ...).
// Same skip rationale as Adult/Sport: the metadata DBs index movies
// and TV, not lectures, so neither path-title fallback nor AI
// resolution can produce useful enrichment.
func isCoursePath(pathStr string) bool {
	return matchesPathFlag(pathStr, func(ti *ptn.TorrentInfo) bool {
		return ti.Course
	})
}

// matchesPathFlag walks each path segment through the parser and
// returns true if `pick` returns true for any of them. Shared
// implementation between isAdultPath and isSportPath.
func matchesPathFlag(pathStr string, pick func(*ptn.TorrentInfo) bool) bool {
	if pathStr == "" {
		return false
	}
	for _, part := range strings.Split(pathStr, "/") {
		if part == "" {
			continue
		}
		ti, err := ptn.Parse(&ptn.TorrentInfo{}, part)
		if err != nil {
			continue
		}
		if pick(ti) {
			return true
		}
	}
	return false
}

// saveResourceMetadata aggregates the per-item ptn outputs into a
// single classification + snapshot row for the resource. is_adult /
// is_sport win on any-true across all items (including samples) —
// the studio folder triggers even when the leaf file looks innocent.
// The representative `metadata` JSONB is the largest video item's
// parsed info, a proxy for "the main feature" in mixed packs.
func (s *Enricher) saveResourceMetadata(ctx context.Context, db *pg.DB, hash string, infos, samples []*TorrentInfo) error {
	all := append(append([]*TorrentInfo{}, infos...), samples...)
	var (
		isAdult bool
		isSport bool
		rep     *ptn.TorrentInfo
		repSize int64
	)
	for _, ti := range all {
		if ti == nil || ti.TorrentInfo == nil {
			continue
		}
		if ti.Adult {
			isAdult = true
		}
		if ti.Sport {
			isSport = true
		}
		if ti.ListItem != nil && ti.ListItem.Size > repSize {
			rep = ti.TorrentInfo
			repSize = ti.ListItem.Size
		}
	}
	if rep == nil && len(all) > 0 && all[0] != nil {
		rep = all[0].TorrentInfo
	}
	rm := &models.ResourceMetadata{
		ResourceID: hash,
		IsAdult:    isAdult,
		IsSport:    isSport,
		Metadata:   ptnAsJSON(rep),
	}
	return models.UpsertResourceMetadata(ctx, db, rm)
}

// ptnAsJSON round-trips a TorrentInfo through encoding/json so the
// stored jsonb mirrors the package's own field names + omitempty
// layout. Cheap (microseconds for a small struct) and keeps the DB
// column shape locked to the public ptn schema rather than re-mapping
// fields by hand.
func ptnAsJSON(ti *ptn.TorrentInfo) map[string]interface{} {
	if ti == nil {
		return nil
	}
	buf, err := json.Marshal(ti)
	if err != nil {
		return nil
	}
	out := map[string]interface{}{}
	if err := json.Unmarshal(buf, &out); err != nil {
		return nil
	}
	return out
}

func (s *Enricher) enrichMediaInfo(ctx context.Context, db *pg.DB, hash string, claims *api.Claims, force bool, hintVideoID string) (*models.MediaInfoMediaType, error) {

	items, err := s.retrieveTorrentItems(ctx, hash, claims)

	if err != nil {
		return nil, err
	}

	//series := map[string]*models.Series{}
	//var movies []*models.Movie
	var torrentInfos []*TorrentInfo
	var samples []*TorrentInfo

	for _, item := range items {
		if item.MediaFormat != ra.Video {
			continue
		}
		ti, err := MakeTorrentInfo(&item)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to make torrent info for hash %v", hash)
		}
		if ti.Sample {
			samples = append(samples, ti)
			continue
		}
		torrentInfos = append(torrentInfos, ti)
	}
	// Drop sample/preview clips when the same torrent already carries the
	// real release. Without this, "Sicario/sicario.sample.mkv" + the main
	// "Sicario.2015...mkv" produce two distinct movie rows and two AI
	// fallback calls. Fall back to processing samples only when nothing
	// else is present (rare — a torrent that is purely a sample).
	if len(torrentInfos) == 0 && len(samples) > 0 {
		log.WithField("hash", hash).Info("only sample files in torrent — processing them as fallback")
		torrentInfos = samples
	} else if len(samples) > 0 {
		log.WithFields(log.Fields{"hash": hash, "dropped": len(samples)}).Info("dropped sample/preview files from enrichment")
	}

	if len(torrentInfos) == 0 {
		log.Infof("no media info acquired for hash %s", hash)
		return nil, nil
	}

	log.Infof("got %v media items", len(torrentInfos))

	// Persist classification + parsed-name snapshot for the whole
	// resource. is_adult / is_sport are OR-aggregated across every
	// video item (including samples) so a clean main file under an
	// adult studio folder still trips the flag at the resource level.
	// Representative metadata is the largest video item — proxies for
	// "the main feature" in mixed packs.
	if err := s.saveResourceMetadata(ctx, db, hash, torrentInfos, samples); err != nil {
		// Non-fatal — classification is best-effort. Adult/sport blur
		// degrades to the default (no blur) until the next enrichment.
		log.WithError(err).WithField("hash", hash).
			Warn("failed to persist resource_metadata")
	}

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

	// One AI-fallback budget shared by every movie + the series block
	// below. Stops a MovieMultiple pack of N unenrichable titles from
	// firing N Claude calls — see resourceAIBudget.
	budget := &resourceAIBudget{}

	for _, m := range movies {
		moviePath := ""
		if m.Path != nil {
			moviePath = *m.Path
		}
		md, err := s.mapMetadata(ctx, m.VideoContent, m.GetContentType(), force, hintVideoID, moviePath, budget)
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
			// Feed AI fallback the torrent ROOT folder, not the first
			// episode filename. A series packaged as
			// "Stand.Up.S13.Complete/01 - haunt in inn.mkv" gives Claude
			// nothing usable from "01 - haunt in inn" but everything it
			// needs from "Stand.Up.S13.Complete". Top-level torrents
			// (single-file series, unusual) keep the full path as-is.
			seriesPath = torrentRoot(seriesPath)
			md, err = s.mapMetadata(ctx, ser.VideoContent, ser.GetContentType(), force, hintVideoID, seriesPath, budget)
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

// GetEnrichedResource returns the persisted enrichment metadata
// (movie first, then series) for a torrent's resource_id, or nil when
// no enrichment row exists yet. Lightweight read — does not trigger
// mapper calls or the AI fallback. Used by the stream-prep flow to:
//
//   - pick the player-overlay title (prefer enriched movie/series
//     name over the raw file basename)
//   - skip thumbnail generation when an IMDb poster is already in
//     place (no point regenerating a worse-quality preview)
//
// Callers that need richer fields (episodes, year, plot) should keep
// reaching for the model loaders directly; this helper intentionally
// returns only the shared VideoMetadata for cheap, hot-path lookups.
func (s *Enricher) GetEnrichedResource(ctx context.Context, resourceID string) (*models.VideoMetadata, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("no db")
	}
	if movie, err := models.GetMovieWithMetadataByResourceID(ctx, db, resourceID); err == nil && movie != nil {
		if md := movie.GetMetadata(); md != nil {
			return md, nil
		}
	}
	if series, err := models.GetSeriesWithMetadataByResourceID(ctx, db, resourceID); err == nil && series != nil {
		if md := series.GetMetadata(); md != nil {
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
	// nil budget — LookupByTitleYear has no pathHint, so AI fallback
	// is never triggered anyway. Pass nil to keep the call site clean.
	return s.mapMetadata(ctx, vc, ct, false, "", "", nil)
}

// EnsureResourceMetadata runs only the classification + parsed-name
// step of the enrich pipeline — retrieve items, parse names, save
// resource_metadata. Skips the heavy mapper / AI / movie-series
// machinery entirely. Used by `enrich run --metadata-only` to
// backfill the classification table for resources enriched before
// this feature shipped, without re-spending the metadata-lookup
// budget those resources already exhausted.
//
// Safe to call repeatedly: the upsert is idempotent on resource_id.
func (s *Enricher) EnsureResourceMetadata(ctx context.Context, hash string, claims *api.Claims) error {
	db := s.pg.Get()
	if db == nil {
		return errors.New("db is nil")
	}
	items, err := s.retrieveTorrentItems(ctx, hash, claims)
	if err != nil {
		// Permanent-rejection errors → purge. The torrent will never
		// be retrievable again, and leaving the row behind makes every
		// subsequent backfill / lookup re-pay the gRPC round-trip to
		// the same dead-end. Run the same purge the resource.banned
		// NATS handler runs so media_info + cascades + per-user tables
		// all clear in one go.
		//
		//   - "found in stoplist": abuse-store CSAM / infringement rule
		//   - "restricted by the rightholder": rightholder takedown
		//     (DMCA, geoblock, etc.). Treated as permanent — historically
		//     takedowns aren't lifted often enough to keep the rows
		//     around "just in case", and the bookkeeping cost of having
		//     dead media_info hashes is permanent.
		if msg := err.Error(); strings.Contains(msg, "found in stoplist") ||
			strings.Contains(msg, "restricted by the rightholder") {
			if purgeErr := models.PurgeResourceByID(ctx, db, hash); purgeErr != nil {
				return errors.Wrapf(purgeErr, "failed to purge blocked hash %s", hash)
			}
			log.WithField("hash", hash).Info("purged blocked resource during classification")
			return nil
		}
		return errors.Wrapf(err, "failed to retrieve items for hash %s", hash)
	}
	var torrentInfos, samples []*TorrentInfo
	for _, item := range items {
		if item.MediaFormat != ra.Video {
			continue
		}
		ti, err := MakeTorrentInfo(&item)
		if err != nil {
			// Per-item parse failures are non-fatal — log + skip,
			// classification stays best-effort.
			log.WithError(err).WithField("hash", hash).
				WithField("path", item.PathStr).
				Warn("failed to parse torrent item for classification")
			continue
		}
		if ti.Sample {
			samples = append(samples, ti)
			continue
		}
		torrentInfos = append(torrentInfos, ti)
	}
	if len(torrentInfos) == 0 && len(samples) == 0 {
		// No video at all — write an empty row anyway so the backfill
		// query doesn't return this hash on every subsequent run.
		return models.UpsertResourceMetadata(ctx, db, &models.ResourceMetadata{
			ResourceID: hash,
		})
	}
	return s.saveResourceMetadata(ctx, db, hash, torrentInfos, samples)
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
//
// budget is a per-resource cap on AI fallback misses; nil disables it.
// See resourceAIBudget for the rationale.
func (s *Enricher) mapMetadata(ctx context.Context, vc *models.VideoContent, t models.ContentType, f bool, hintVideoID string, pathHint string, budget *resourceAIBudget) (*models.VideoMetadata, error) {
	if hintVideoID != "" {
		if md := s.lookupByHint(ctx, hintVideoID, t, f); md != nil {
			return md, nil
		}
	}
	md, firstErr := s.searchAllMappers(ctx, vc, t, f)
	if md != nil {
		return md, nil
	}
	// Path-title candidates: parseItem harvested the Title from every
	// segment of the source torrent path into Metadata["path_titles"].
	// Try each as a search key before resorting to Claude — the
	// existing TMDB.query / KPU.query caches absorb the repeated
	// per-title lookups so this is cheap even when it misses.
	//
	// Guarded:
	//   - Adult / Sport paths are skipped (same rationale as the AI
	//     fallback — these never resolve through TMDB/OMDB/KPU, and
	//     short adult-site tokens like "FC2" / "SLR" can accidentally
	//     match an obscure unrelated film when probed directly).
	//   - Minimum 3 characters — single-letter and 2-letter path
	//     segments ("D", "HQ", "NF") have too high a TMDB collision
	//     rate to be safe candidates.
	if pathHint == "" || (!isAdultPath(pathHint) && !isSportPath(pathHint) && !isCoursePath(pathHint)) {
		for _, pt := range extractPathTitles(vc) {
			if len(pt) < 3 || pt == vc.Title {
				continue
			}
			candVC := &models.VideoContent{
				ResourceID: vc.ResourceID,
				Title:      pt,
				Year:       vc.Year,
			}
			if cmd, _ := s.searchAllMappers(ctx, candVC, t, f); cmd != nil {
				log.WithFields(log.Fields{
					"candidate":   pt,
					"resolved_id": cmd.VideoID,
				}).Info("metadata resolved via path-title candidate (no AI call)")
				return cmd, nil
			}
		}
	}
	if s.aiResolver != nil && pathHint != "" {
		if aiMD := s.tryAIFallback(ctx, vc, t, f, pathHint, budget); aiMD != nil {
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

// extractPathTitles pulls the parser-harvested per-segment Title
// candidates back out of VideoContent.Metadata. The values live in
// the same json blob that StructToMap writes from the underlying
// TorrentInfo (see makeMovie / makeSeriesWithEpisodes), so no extra
// transient field on VideoContent is required — DB persistence is
// a free side effect of the existing metadata round-trip.
func extractPathTitles(vc *models.VideoContent) []string {
	if vc == nil || vc.Metadata == nil {
		return nil
	}
	raw, ok := vc.Metadata["path_titles"]
	if !ok {
		return nil
	}
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
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
func (s *Enricher) tryAIFallback(ctx context.Context, vc *models.VideoContent, t models.ContentType, f bool, pathHint string, budget *resourceAIBudget) *models.VideoMetadata {
	// Hard stop on prior miss — see resourceAIBudget.failed.
	if budget != nil && budget.failed {
		log.WithField("path", pathHint).Info("ai_enrich: budget marked failed for this resource, skipping AI fallback")
		return nil
	}
	// Skip Claude entirely for paths flagged as adult by the torrent-name
	// parser (studio names, JAV codes, explicit keywords). Adult content
	// is not resolvable through TMDB/OMDB/KPU and was filling the
	// ai_enrich.query negative cache with ~30-40% pure waste — see
	// docs/ai_enrichment.md.
	if isAdultPath(pathHint) {
		log.WithField("path", pathHint).Info("ai_enrich: skipping adult path")
		return nil
	}
	// Same rationale for sports broadcasts (NBA, NHL, WWE, AEW, UFC,
	// Premier League, КХЛ, ...). The metadata DBs index movies/series,
	// not broadcasts; Claude has nothing to suggest. See the Sport
	// transient flag in services/parse_torrent_name.
	if isSportPath(pathHint) {
		log.WithField("path", pathHint).Info("ai_enrich: skipping sport path")
		return nil
	}
	// Same rationale for pirate-course / e-learning content.
	if isCoursePath(pathHint) {
		log.WithField("path", pathHint).Info("ai_enrich: skipping course path")
		return nil
	}
	// Shape-based garbage filter — pure IDs, hashes, release-group salt
	// ("etrg", "frgo"), random alphanumerics. See garbage.go for the
	// signals. Calibrated over 4578-row corpus.
	//
	// Skip ONLY when both the parsed title AND every path-segment
	// title look like noise — Claude receives both pathHint and the
	// title, and can still extract a real name from a clean parent
	// directory even when the file's own title is a hash. Conversely,
	// if every candidate string is garbage there's nothing for Claude
	// to disambiguate.
	if isGarbageTitle(vc.Title) {
		allGarbage := true
		for _, pt := range extractPathTitles(vc) {
			if !isGarbageTitle(pt) {
				allGarbage = false
				break
			}
		}
		if allGarbage {
			log.WithField("title", vc.Title).Info("ai_enrich: skipping likely-garbage title + path-titles")
			return nil
		}
	}
	// Locked candidates — two consecutive Claude calls returned the
	// SAME set, so the parser is almost certainly seeing N "different"
	// titles for one logical work (Dragon Ball Clássico 001…153 → all
	// resolve to "Dragon Ball"). Reuse without re-calling Claude. If
	// the locked set fails to resolve for THIS file, fall through to a
	// fresh Claude call below (the file may be a legit-different work
	// in a multi-movie pack that only happened to overlap on two files).
	if budget != nil && budget.locked != nil {
		if md := s.runCandidates(ctx, vc, t, f, budget.locked); md != nil {
			log.WithField("path", pathHint).Info("ai_enrich: resolved via locked candidates (no Claude call)")
			return md
		}
		// Locked set didn't match this file — fall through.
	}
	// Cap on fresh Claude calls per resource. Above the cap, no more
	// calls regardless of why (success, miss, or locked-fall-through).
	if !budget.available() {
		log.WithField("path", pathHint).Info("ai_enrich: budget cap reached for this resource, skipping AI fallback")
		return nil
	}
	candidates := s.aiResolver.SuggestCandidates(ctx, vc.ResourceID, pathHint, vc.Title, vc.Year, t, f)
	budget.recordCall(candidates)
	if md := s.runCandidates(ctx, vc, t, f, candidates); md != nil {
		return md
	}
	// No candidate produced a metadata hit. Mark the resource budget
	// failed so other files in this same torrent don't repeat the
	// experiment. Successful runs return above and never reach here.
	budget.markFailed()
	return nil
}

// runCandidates walks `cands` through the mapper chain and returns the
// first match. Shared between the locked-candidates fast path and the
// regular post-Claude path so both behave identically downstream.
func (s *Enricher) runCandidates(ctx context.Context, vc *models.VideoContent, t models.ContentType, f bool, cands []TitleCandidate) *models.VideoMetadata {
	for _, cand := range cands {
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

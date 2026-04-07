package models

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	"golang.org/x/sync/errgroup"
)

type WatchHistory struct {
	tableName struct{} `pg:"watch_history"`

	UserID     uuid.UUID `pg:"user_id,pk"`
	ResourceID string    `pg:"resource_id,pk"`
	Path       string    `pg:"path,pk"`
	Position   float32   `pg:"position"`
	Duration   float32   `pg:"duration"`
	Watched    bool      `pg:"watched"`
	CreatedAt  time.Time `pg:"created_at"`
	UpdatedAt  time.Time `pg:"updated_at"`

	Torrent *TorrentResource `pg:"rel:has-one,fk:resource_id"`

	// Enriched fields (not stored in DB)
	Title       string      `pg:"-"`
	PosterURL   string      `pg:"-"`
	VideoID     string      `pg:"-"`
	ContentType ContentType `pg:"-"`
}

// Progress returns watch progress as percentage (0-100).
func (wh *WatchHistory) Progress() int {
	if wh.Duration <= 0 {
		return 0
	}
	p := int(wh.Position / wh.Duration * 100)
	if p > 100 {
		p = 100
	}
	return p
}

// DisplayName returns the enriched title or torrent name.
func (wh *WatchHistory) DisplayName() string {
	if wh.Title != "" {
		return wh.Title
	}
	if wh.Torrent != nil && wh.Torrent.Name != "" {
		return wh.Torrent.Name
	}
	return ""
}

// UpsertWatchPosition writes the player position and returns transitioned=true
// when this upsert is the one that flipped the `watched` flag from false to
// true (i.e. the user has just crossed the 90% threshold for the first time).
// Callers use this to trigger the IMDB-level auto-mark into user_video_status
// exactly once, not on every subsequent 90%+ position frame.
func UpsertWatchPosition(ctx context.Context, db *pg.DB, wh *WatchHistory) (transitioned bool, err error) {
	watched := wh.Duration > 0 && wh.Position/wh.Duration >= 0.9
	wh.Watched = watched

	// Read prior state (PK lookup, negligible cost) so we can detect the
	// false → true transition. If no prior row exists, wasWatched stays false.
	var prev WatchHistory
	prevErr := db.Model(&prev).
		Context(ctx).
		Column("watched").
		Where("user_id = ? AND resource_id = ? AND path = ?", wh.UserID, wh.ResourceID, wh.Path).
		Limit(1).
		Select()
	if prevErr != nil && !errors.Is(prevErr, pg.ErrNoRows) {
		return false, errors.Wrap(prevErr, "failed to load prior watch position")
	}
	wasWatched := prevErr == nil && prev.Watched

	_, err = db.Model(wh).
		Context(ctx).
		OnConflict("(user_id, resource_id, path) DO UPDATE").
		Set("position = EXCLUDED.position").
		Set("duration = EXCLUDED.duration").
		Set("watched = EXCLUDED.position / NULLIF(EXCLUDED.duration, 0) >= 0.9").
		Insert()
	if err != nil {
		return false, errors.Wrap(err, "failed to upsert watch position")
	}
	return watched && !wasWatched, nil
}

func GetWatchPosition(ctx context.Context, db *pg.DB, userID uuid.UUID, resourceID string, path string) (*WatchHistory, error) {
	var wh WatchHistory
	err := db.Model(&wh).
		Context(ctx).
		Where("user_id = ? AND resource_id = ? AND path = ?", userID, resourceID, path).
		Limit(1).
		Select()
	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to get watch position")
	}
	return &wh, nil
}

// GetRecentlyWatched returns one entry per resource (the most recently updated unwatched file),
// enriched with movie/series metadata for display names and posters.
func GetRecentlyWatched(ctx context.Context, db *pg.DB, userID uuid.UUID, limit int) ([]*WatchHistory, error) {
	// Phase 1: fetch unwatched entries and watched-resource aggregates in parallel.
	var list []*WatchHistory
	var watchedResources []struct {
		ResourceID string    `pg:"resource_id"`
		UpdatedAt  time.Time `pg:"updated_at"`
	}

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return db.Model(&list).
			Context(gctx).
			DistinctOn("watch_history.resource_id").
			Where("watch_history.user_id = ?", userID).
			Where("watch_history.watched = false").
			Where("watch_history.duration > 0").
			Relation("Torrent").
			OrderExpr("watch_history.resource_id, watch_history.updated_at DESC").
			Select()
	})

	g.Go(func() error {
		return db.ModelContext(gctx, (*WatchHistory)(nil)).
			ColumnExpr("resource_id, MAX(updated_at) AS updated_at").
			Where("user_id = ?", userID).
			Where("watched = true").
			Group("resource_id").
			Select(&watchedResources)
	})

	if err := g.Wait(); err != nil {
		return nil, errors.Wrap(err, "failed to fetch recently watched")
	}

	// Build candidate list for next-episode detection.
	seen := make(map[string]bool, len(list))
	for _, wh := range list {
		seen[wh.ResourceID] = true
	}

	var candidateIDs []string
	updatedMap := make(map[string]time.Time)
	for _, wr := range watchedResources {
		if !seen[wr.ResourceID] {
			candidateIDs = append(candidateIDs, wr.ResourceID)
			updatedMap[wr.ResourceID] = wr.UpdatedAt
		}
	}

	if len(candidateIDs) > 0 {
		// Phase 2: load episodes and watched paths in parallel.
		var episodes []*Episode
		var watchedPaths []WatchHistory

		g, gctx = errgroup.WithContext(ctx)

		g.Go(func() error {
			return db.ModelContext(gctx, &episodes).
				Where("resource_id IN (?)", pg.In(candidateIDs)).
				Where("path IS NOT NULL").
				OrderExpr("resource_id, season NULLS LAST, episode NULLS LAST").
				Select()
		})

		g.Go(func() error {
			return db.ModelContext(gctx, &watchedPaths).
				Column("resource_id", "path", "watched").
				Where("user_id = ?", userID).
				Where("resource_id IN (?)", pg.In(candidateIDs)).
				Select()
		})

		if err := g.Wait(); err != nil {
			return nil, errors.Wrap(err, "failed to fetch episode data")
		}

		watchedSet := make(map[string]bool)
		for _, wp := range watchedPaths {
			watchedSet[wp.ResourceID+":"+wp.Path] = wp.Watched
		}

		// Find next episode per resource.
		epsByResource := make(map[string][]*Episode)
		for _, ep := range episodes {
			epsByResource[ep.ResourceID] = append(epsByResource[ep.ResourceID], ep)
		}

		var nextResourceIDs []string
		for resourceID, eps := range epsByResource {
			lastWatchedIdx := -1
			for i, ep := range eps {
				key := resourceID + ":" + *ep.Path
				if watched, ok := watchedSet[key]; ok && watched {
					lastWatchedIdx = i
				}
			}
			if lastWatchedIdx < 0 || lastWatchedIdx >= len(eps)-1 {
				continue
			}
			nextEp := eps[lastWatchedIdx+1]
			list = append(list, &WatchHistory{
				UserID:     userID,
				ResourceID: nextEp.ResourceID,
				Path:       *nextEp.Path,
				UpdatedAt:  updatedMap[nextEp.ResourceID],
			})
			nextResourceIDs = append(nextResourceIDs, nextEp.ResourceID)
		}

		// Phase 3: load torrents for next-episode entries.
		if len(nextResourceIDs) > 0 {
			var torrents []*TorrentResource
			_ = db.ModelContext(ctx, &torrents).
				Where("resource_id IN (?)", pg.In(nextResourceIDs)).
				Select()
			tMap := make(map[string]*TorrentResource, len(torrents))
			for _, t := range torrents {
				tMap[t.ResourceID] = t
			}
			for _, wh := range list {
				if wh.Torrent == nil {
					wh.Torrent = tMap[wh.ResourceID]
				}
			}
		}
	}

	if len(list) == 0 {
		return nil, nil
	}

	// Sort by updated_at DESC (DISTINCT ON requires ORDER BY resource_id first).
	sort.Slice(list, func(i, j int) bool {
		return list[i].UpdatedAt.After(list[j].UpdatedAt)
	})

	if len(list) > limit {
		list = list[:limit]
	}

	// Phase 4: enrich with metadata — movies and series in parallel.
	resourceIDs := make([]string, len(list))
	for i, wh := range list {
		resourceIDs[i] = wh.ResourceID
	}
	enrichWatchHistoryParallel(ctx, db, list, resourceIDs)

	// Phase 5: filter out fully-watched — movie_status and series_status in parallel.
	list = filterOutFullyWatched(ctx, db, list, userID)

	if len(list) == 0 {
		return nil, nil
	}

	return list, nil
}

type enrichedMeta struct {
	ResourceID  string      `pg:"resource_id"`
	Title       string      `pg:"title"`
	PosterURL   string      `pg:"poster_url"`
	VideoID     string      `pg:"video_id"`
	ContentType ContentType `pg:"-"`
}

// enrichWatchHistoryParallel fills Title and PosterURL from movie/series data,
// fetching both in parallel.
func enrichWatchHistoryParallel(ctx context.Context, db *pg.DB, list []*WatchHistory, resourceIDs []string) {
	var movies []enrichedMeta
	var series []enrichedMeta

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return db.ModelContext(gctx, (*Movie)(nil)).
			ColumnExpr("movie.resource_id").
			ColumnExpr("COALESCE(mmd.title, movie.title) AS title").
			ColumnExpr("mmd.poster_url").
			ColumnExpr("mmd.video_id").
			Join("LEFT JOIN movie_metadata AS mmd ON mmd.movie_metadata_id = movie.movie_metadata_id").
			Where("movie.resource_id IN (?)", pg.In(resourceIDs)).
			Select(&movies)
	})

	g.Go(func() error {
		return db.ModelContext(gctx, (*Series)(nil)).
			ColumnExpr("series.resource_id").
			ColumnExpr("COALESCE(smd.title, series.title) AS title").
			ColumnExpr("smd.poster_url").
			ColumnExpr("smd.video_id").
			Join("LEFT JOIN series_metadata AS smd ON smd.series_metadata_id = series.series_metadata_id").
			Where("series.resource_id IN (?)", pg.In(resourceIDs)).
			Select(&series)
	})

	_ = g.Wait()

	metaMap := make(map[string]*enrichedMeta)
	for i := range movies {
		movies[i].ContentType = ContentTypeMovie
		metaMap[movies[i].ResourceID] = &movies[i]
	}
	for i := range series {
		if _, ok := metaMap[series[i].ResourceID]; !ok {
			series[i].ContentType = ContentTypeSeries
			metaMap[series[i].ResourceID] = &series[i]
		}
	}

	for _, wh := range list {
		if m, ok := metaMap[wh.ResourceID]; ok {
			wh.Title = m.Title
			wh.VideoID = m.VideoID
			wh.ContentType = m.ContentType
			if m.PosterURL != "" && m.VideoID != "" {
				wh.PosterURL = fmt.Sprintf("/lib/%s/poster/%s/240.jpg", m.ContentType, m.VideoID)
			}
		}
	}
}

// filterOutFullyWatched removes items from the list whose corresponding
// movie/series has been marked as fully watched in user_video_status (either
// manually by the user or automatically after completing all episodes). The
// user declared the work finished — it should not appear in "continue
// watching" even if individual files still have sub-90% progress.
func filterOutFullyWatched(ctx context.Context, db *pg.DB, list []*WatchHistory, userID uuid.UUID) []*WatchHistory {
	var movieIDs, seriesIDs []string
	for _, wh := range list {
		if wh.VideoID == "" {
			continue
		}
		switch wh.ContentType {
		case ContentTypeMovie:
			movieIDs = append(movieIDs, wh.VideoID)
		case ContentTypeSeries:
			seriesIDs = append(seriesIDs, wh.VideoID)
		}
	}

	watchedMovies := map[string]bool{}
	watchedSeries := map[string]bool{}

	g, gctx := errgroup.WithContext(ctx)

	if len(movieIDs) > 0 {
		g.Go(func() error {
			if m, err := GetMovieStatusMap(gctx, db, userID, movieIDs); err == nil {
				for vid, st := range m {
					if st.Watched {
						watchedMovies[vid] = true
					}
				}
			}
			return nil
		})
	}
	if len(seriesIDs) > 0 {
		g.Go(func() error {
			if m, err := GetSeriesStatusMap(gctx, db, userID, seriesIDs); err == nil {
				for vid, st := range m {
					if st.Watched {
						watchedSeries[vid] = true
					}
				}
			}
			return nil
		})
	}

	_ = g.Wait()

	if len(watchedMovies) == 0 && len(watchedSeries) == 0 {
		return list
	}
	filtered := list[:0]
	for _, wh := range list {
		if wh.VideoID != "" {
			if wh.ContentType == ContentTypeMovie && watchedMovies[wh.VideoID] {
				continue
			}
			if wh.ContentType == ContentTypeSeries && watchedSeries[wh.VideoID] {
				continue
			}
		}
		filtered = append(filtered, wh)
	}
	return filtered
}

// GetWatchedPaths returns a map of path -> watched for all entries of a resource.
func GetWatchedPaths(ctx context.Context, db *pg.DB, userID uuid.UUID, resourceID string) (map[string]bool, error) {
	var list []WatchHistory
	err := db.Model(&list).
		Context(ctx).
		Column("path", "watched").
		Where("user_id = ? AND resource_id = ?", userID, resourceID).
		Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get watched paths")
	}
	result := make(map[string]bool, len(list))
	for _, wh := range list {
		result[wh.Path] = wh.Watched
	}
	return result, nil
}

func DeleteWatchHistory(ctx context.Context, db *pg.DB, userID uuid.UUID, resourceID string, path string) error {
	_, err := db.Model((*WatchHistory)(nil)).
		Context(ctx).
		Where("user_id = ? AND resource_id = ? AND path = ?", userID, resourceID, path).
		Delete()
	if err != nil {
		return errors.Wrap(err, "failed to delete watch history")
	}
	return nil
}

// SetWatchedForMovie flips watch_history.watched for every file a user has
// played that maps (via movie + movie_metadata enrichment) to the given IMDB
// video_id. Used by the user_video_status service to keep the per-file
// resume/ribbon model consistent with a manual IMDB-level mark or unmark.
// No-op (0 rows affected) when the user has never played any file of the movie.
func SetWatchedForMovie(ctx context.Context, db *pg.DB, userID uuid.UUID, videoID string, watched bool) error {
	_, err := db.Model((*WatchHistory)(nil)).
		Context(ctx).
		Set("watched = ?", watched).
		Where("user_id = ?", userID).
		Where(`EXISTS (
			SELECT 1 FROM movie m
			JOIN movie_metadata mm ON mm.movie_metadata_id = m.movie_metadata_id
			WHERE m.resource_id = watch_history.resource_id
				AND mm.video_id = ?
				AND (m.path IS NULL OR m.path = watch_history.path)
		)`, videoID).
		Update()
	if err != nil {
		return errors.Wrap(err, "failed to sync watch_history for movie")
	}
	return nil
}

// SetWatchedForEpisode flips watch_history.watched for every file that maps
// (via episode + series_metadata enrichment) to the given IMDB video_id at
// the specified season / episode. Cross-torrent by design: if the user has
// multiple releases of the same show, every corresponding file gets updated.
func SetWatchedForEpisode(ctx context.Context, db *pg.DB, userID uuid.UUID, videoID string, season, episode int16, watched bool) error {
	_, err := db.Model((*WatchHistory)(nil)).
		Context(ctx).
		Set("watched = ?", watched).
		Where("user_id = ?", userID).
		Where(`EXISTS (
			SELECT 1 FROM episode e
			JOIN series s ON s.series_id = e.series_id
			JOIN series_metadata sm ON sm.series_metadata_id = s.series_metadata_id
			WHERE e.resource_id = watch_history.resource_id
				AND e.path = watch_history.path
				AND sm.video_id = ?
				AND e.season = ?
				AND e.episode = ?
		)`, videoID, season, episode).
		Update()
	if err != nil {
		return errors.Wrap(err, "failed to sync watch_history for episode")
	}
	return nil
}

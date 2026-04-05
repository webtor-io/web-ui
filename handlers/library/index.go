package library

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/handlers/library/shared"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/web"
)

type IndexData struct {
	Args          *shared.IndexArgs
	Items         []any
	TorrentCount  int
	MovieCount    int
	SeriesCount   int
}

func (s *Handler) bindIndexArgs(c *gin.Context) (args *shared.IndexArgs) {
	args = &shared.IndexArgs{}
	if c.Query("sort") == "" {
		args.Sort = models.SortTypeRecentlyAdded
	} else {
		if ss, err := strconv.Atoi(c.Query("sort")); err == nil {
			args.Sort = models.SortType(ss)
		}
	}
	if c.Param("type") == "" {
		args.Section = shared.SectionTypeTorrents
	} else {
		args.Section = shared.SectionType(c.Param("type"))
	}
	switch shared.WatchedFilter(c.Query("watched")) {
	case shared.WatchedFilterUnwatched:
		args.Watched = shared.WatchedFilterUnwatched
	case shared.WatchedFilterWatched:
		args.Watched = shared.WatchedFilterWatched
	default:
		args.Watched = shared.WatchedFilterAll
	}
	return
}

func (s *Handler) index(c *gin.Context) {
	u := auth.GetUserFromContext(c)
	if !u.HasAuth() {
		v := url.Values{
			"from":       []string{"library"},
			"return-url": []string{"/lib/"},
		}
		c.Redirect(http.StatusFound, "/login?"+v.Encode())
		return
	}
	args := s.bindIndexArgs(c)

	db := s.pg.Get()
	if db == nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.New("no db"))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	ls, err := s.getLibraryList(ctx, u, args)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get library list"))
		return
	}

	// Annotate each item with UserWatched flag (bulk prefetch to avoid N+1).
	s.annotateWatched(ctx, db, u.ID, ls)

	tc, mc, sc, err := models.GetLibraryCounts(ctx, db, u.ID)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get library counts"))
		return
	}

	indexData := &IndexData{
		Args:         args,
		Items:        ls,
		TorrentCount: tc,
		MovieCount:   mc,
		SeriesCount:  sc,
	}

	s.tb.Build("library/index").HTML(http.StatusOK, web.NewContext(c).WithData(indexData))
}

func (s *Handler) getLibraryList(ctx context.Context, u *auth.User, args *shared.IndexArgs) (items []any, err error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("no db")
	}
	switch args.Section {
	case shared.SectionTypeTorrents:
		return s.getTorrentList(ctx, u.ID, db, args.Sort)
	case shared.SectionTypeMovies:
		return s.getMovieList(ctx, u.ID, db, args.Sort, args.Watched)
	case shared.SectionTypeSeries:
		return s.getSeriesList(ctx, u.ID, db, args.Sort, args.Watched)
	}
	return
}

func (s *Handler) getTorrentList(ctx context.Context, id uuid.UUID, db *pg.DB, sort models.SortType) (items []any, err error) {
	ls, err := models.GetLibraryTorrentsList(ctx, db, id, sort)
	if err != nil {
		return
	}
	items = make([]any, len(ls))
	for i, v := range ls {
		items[i] = v
	}
	return
}

func (s *Handler) getMovieList(ctx context.Context, id uuid.UUID, db *pg.DB, sort models.SortType, watched shared.WatchedFilter) (items []any, err error) {
	ls, err := models.GetLibraryMovieList(ctx, db, id, sort, string(watched))
	if err != nil {
		return
	}
	items = make([]any, len(ls))
	for i, v := range ls {
		items[i] = v
	}
	return
}

func (s *Handler) getSeriesList(ctx context.Context, id uuid.UUID, db *pg.DB, sort models.SortType, watched shared.WatchedFilter) (items []any, err error) {
	ls, err := models.GetLibrarySeriesList(ctx, db, id, sort, string(watched))
	if err != nil {
		return
	}
	items = make([]any, len(ls))
	for i, v := range ls {
		items[i] = v
	}
	return
}

// annotateWatched sets the transient UserWatched flag on each Movie/Series in
// items, using a single bulk query per kind against movie_status /
// series_status. Errors are swallowed: a missing watched badge is a
// display-only concern and must not fail rendering of the library page.
func (s *Handler) annotateWatched(ctx context.Context, db *pg.DB, userID uuid.UUID, items []any) {
	if len(items) == 0 {
		return
	}
	var movieIDs []string
	var seriesIDs []string
	movieByVideoID := map[string]*models.Movie{}
	seriesByVideoID := map[string]*models.Series{}

	for _, it := range items {
		switch v := it.(type) {
		case *models.Movie:
			if v.MovieMetadata != nil && v.MovieMetadata.VideoID != "" {
				movieIDs = append(movieIDs, v.MovieMetadata.VideoID)
				movieByVideoID[v.MovieMetadata.VideoID] = v
			}
		case *models.Series:
			if v.SeriesMetadata != nil && v.SeriesMetadata.VideoID != "" {
				seriesIDs = append(seriesIDs, v.SeriesMetadata.VideoID)
				seriesByVideoID[v.SeriesMetadata.VideoID] = v
			}
		}
	}

	if len(movieIDs) > 0 {
		if m, err := models.GetMovieStatusMap(ctx, db, userID, movieIDs); err == nil {
			for vid, status := range m {
				if status.Watched {
					if mv := movieByVideoID[vid]; mv != nil {
						mv.UserWatched = true
					}
				}
			}
		}
	}
	if len(seriesIDs) > 0 {
		if m, err := models.GetSeriesStatusMap(ctx, db, userID, seriesIDs); err == nil {
			for vid, status := range m {
				if status.Watched {
					if sv := seriesByVideoID[vid]; sv != nil {
						sv.UserWatched = true
					}
				}
			}
		}
	}
}

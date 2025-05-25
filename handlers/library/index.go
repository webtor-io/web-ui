package library

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/handlers/library/shared"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/web"
	"net/http"
	"strconv"
)

type IndexData struct {
	Args  *shared.IndexArgs
	Items []any
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
	return
}

func (s *Handler) index(c *gin.Context) {
	u := auth.GetUserFromContext(c)
	if !u.HasAuth() {
		c.Redirect(http.StatusFound, "/login?from=library")
		return
	}
	args := s.bindIndexArgs(c)

	ls, err := s.getLibraryList(c.Request.Context(), u, args)

	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	indexData := &IndexData{
		Args:  args,
		Items: ls,
	}

	s.tb.Build("library/index").HTML(http.StatusOK, web.NewContext(c).WithData(indexData))
}

func (s *Handler) getLibraryList(ctx context.Context, u *auth.User, args *shared.IndexArgs) (items []any, err error) {
	uID, err := uuid.FromString(u.ID)
	if err != nil {
		return
	}
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("no db")
	}
	switch args.Section {
	case shared.SectionTypeTorrents:
		return s.getTorrentList(ctx, uID, db, args.Sort)
	case shared.SectionTypeMovies:
		return s.getMovieList(ctx, uID, db, args.Sort)
	case shared.SectionTypeSeries:
		return s.getSeriesList(ctx, uID, db, args.Sort)
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

func (s *Handler) getMovieList(ctx context.Context, id uuid.UUID, db *pg.DB, sort models.SortType) (items []any, err error) {
	ls, err := models.GetLibraryMovieList(ctx, db, id, sort)
	if err != nil {
		return
	}
	items = make([]any, len(ls))
	for i, v := range ls {
		items[i] = v
	}
	return
}

func (s *Handler) getSeriesList(ctx context.Context, id uuid.UUID, db *pg.DB, sort models.SortType) (items []any, err error) {
	ls, err := models.GetLibrarySeriesList(ctx, db, id, sort)
	if err != nil {
		return
	}
	items = make([]any, len(ls))
	for i, v := range ls {
		items[i] = v
	}
	return
}

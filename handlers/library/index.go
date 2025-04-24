package library

import (
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/models"
	"github.com/webtor-io/web-ui/services/web"
	"net/http"
)

type IndexArgs struct {
	Sort models.LibrarySort
}
type IndexData struct {
	Args  *IndexArgs
	Items []*models.Library
}

func (s *Handler) bindIndexArgs(c *gin.Context) (args *IndexArgs) {
	args = &IndexArgs{}
	if c.Query("sort") == "name" {
		args.Sort = models.SortByName
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

	ls, err := s.getLibraryList(u, args)

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

func (s *Handler) getLibraryList(u *auth.User, args *IndexArgs) (ls []*models.Library, err error) {
	uID, err := uuid.FromString(u.ID)
	if err != nil {
		return
	}
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("no db")
	}
	return models.GetLibraryList(db, uID, args.Sort)
}

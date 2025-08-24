package resource

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	sv "github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/web"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/services/api"
)

var (
	sampleReg = regexp.MustCompile("/sample/i")
)

const (
	pageSize = 25
)

type GetArgs struct {
	ID       string
	Query    string
	Page     uint
	PageSize uint
	PWD      string
	File     string
	Claims   *api.Claims
	User     *auth.User
}

func (s *Handler) bindGetArgs(c *gin.Context) (*GetArgs, error) {
	id := c.Param("resource_id")
	sha1 := sv.SHA1R.Find([]byte(id))
	if sha1 == nil {
		return nil, errors.Errorf("wrong resource provided resource_id=%v", id)
	}
	page := uint(1)
	if c.Query("page") != "" {
		p, err := strconv.Atoi(c.Query("page"))
		if err == nil && p > 1 {
			page = uint(p)
		}
	}

	return &GetArgs{
		ID:       id,
		Page:     page,
		PageSize: pageSize,
		PWD:      c.Query("pwd"),
		File:     c.Query("file"),
		Claims:   api.GetClaimsFromContext(c),
		User:     auth.GetUserFromContext(c),
	}, nil
}

func (s *Handler) getList(ctx context.Context, args *GetArgs) (l *ra.ListResponse, err error) {
	limit := args.PageSize
	offset := (args.Page - 1) * args.PageSize
	l, err = s.api.ListResourceContentCached(ctx, args.Claims, args.ID, &api.ListResourceContentArgs{
		Output: api.OutputTree,
		Path:   args.PWD,
		Limit:  limit,
		Offset: offset,
	})
	return
}

type GetData struct {
	Args        *GetArgs
	Resource    *ExtendedResource
	List        *ra.ListResponse
	Item        *ra.ListItem
	Instruction string
}

type ExtendedResource struct {
	*ra.ResourceResponse
	InLibrary bool
}

func (s *Handler) prepareGetData(ctx context.Context, args *GetArgs) (*GetData, error) {
	var (
		res  *ra.ResourceResponse
		list *ra.ListResponse
		err  error
	)
	d := &GetData{
		Args: args,
	}
	res, err = s.api.GetResource(ctx, args.Claims, args.ID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get resource")
	}
	d.Resource = &ExtendedResource{
		ResourceResponse: res,
	}
	if res == nil {
		return nil, nil
	}
	list, err = s.getList(ctx, args)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list resource")
	}
	if len(list.Items) == 1 && list.Items[0].Type == ra.ListTypeDirectory {
		args.PWD = list.Items[0].PathStr
		list, err = s.getList(ctx, args)
		if err != nil {
			return nil, errors.Wrap(err, "failed to list resource")
		}
	}
	if len(list.Items) > 1 {
		d.List = list
	}
	d.Item, err = s.getBestItem(ctx, list, args)
	if len(list.Items) == 1 && d.Item == nil {
		d.List = list
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to get item")
	}

	if args.User.HasAuth() {
		db := s.pg.Get()
		if db == nil {
			return nil, errors.New("failed to connect to database")
		}
		d.Resource.InLibrary, err = models.IsInLibrary(ctx, db, args.User.ID, d.Resource.ID)
		if err != nil {
			return nil, errors.Wrap(err, "failed to check if resource is in-library")
		}
	}
	return d, nil
}

func (s *Handler) get(c *gin.Context) {
	indexTpl := s.tb.Build("index")
	getTpl := s.tb.Build("resource/get")
	args, err := s.bindGetArgs(c)
	if err != nil {
		indexTpl.HTML(http.StatusBadRequest, web.NewContext(c).WithData(&GetData{}).WithErr(errors.Wrap(err, "wrong args provided")))
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	d, err := s.prepareGetData(ctx, args)
	if err != nil {
		indexTpl.HTML(http.StatusInternalServerError, web.NewContext(c).WithData(&GetData{}).WithErr(err))
		return
	}
	if d == nil {
		indexTpl.HTML(http.StatusNotFound, web.NewContext(c).WithData(d).WithErr(errors.Wrap(err, "resource not found")))
		return
	}
	getTpl.HTML(http.StatusOK, web.NewContext(c).WithData(d))
}

func (s *Handler) getBestItem(ctx context.Context, l *ra.ListResponse, args *GetArgs) (i *ra.ListItem, err error) {
	if args.File != "" {
		for _, v := range l.Items {
			if v.PathStr == args.File {
				i = &v
				return
			}
		}
		l, err = s.api.ListResourceContentCached(ctx, args.Claims, args.ID, &api.ListResourceContentArgs{
			Path: args.File,
		})
		if err != nil {
			return
		}
		if len(l.Items) > 0 {
			i = &l.Items[0]
			return
		}
	}
	if args.Page == 1 {
		for _, v := range l.Items {
			if v.MediaFormat == ra.Video && !sampleReg.MatchString(v.Name) {
				i = &v
				return
			}
		}
		for _, v := range l.Items {
			if v.MediaFormat == ra.Audio && !sampleReg.MatchString(v.Name) {
				i = &v
				return
			}
		}
		for _, v := range l.Items {
			if v.Type == ra.ListTypeFile {
				i = &v
				return
			}
		}
	}
	return
}

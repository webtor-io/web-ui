package resource

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
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
	FileIdx  *int
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

	var fileIdx *int
	if fi := c.Query("file-idx"); fi != "" {
		if v, err := strconv.Atoi(fi); err == nil && v >= 0 {
			fileIdx = &v
		}
	}

	return &GetArgs{
		ID:       id,
		Page:     page,
		PageSize: pageSize,
		PWD:      c.Query("pwd"),
		File:     c.Query("file"),
		FileIdx:  fileIdx,
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
		Sort:   "name",
	})
	return
}

type GetData struct {
	Args                  *GetArgs
	Resource              *ExtendedResource
	List                  *ra.ListResponse
	Item                  *ra.ListItem
	VaultPledgeAddForm    *VaultPledgeAddForm
	VaultButton           *VaultButton
	VaultPledgeRemoveForm *VaultPledgeRemoveForm
	Vault                 bool
	TorrentStatus         *TorrentStatus
	Movie                 *models.Movie
	Series                *models.Series
	WatchedPaths          map[string]bool
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
	if args.FileIdx != nil && args.File == "" {
		path, err := s.resolveFileIdx(ctx, args, *args.FileIdx)
		if err == nil && path != "" {
			args.File = path
			// Set PWD to the parent directory of the resolved file
			if dir := filepath.Dir(path); dir != "/" && dir != "." {
				args.PWD = dir
				list, err = s.getList(ctx, args)
				if err != nil {
					return nil, errors.Wrap(err, "failed to list resource after fileIdx resolution")
				}
				d.List = list
			}
		}
	}
	d.Item, err = s.getBestItem(ctx, list, args)
	if len(list.Items) == 1 && d.Item == nil {
		d.List = list
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to get item")
	}

	db := s.pg.Get()
	if db != nil {
		if args.User.HasAuth() {
			d.Resource.InLibrary, err = models.IsInLibrary(ctx, db, args.User.ID, d.Resource.ID)
			if err != nil {
				return nil, errors.Wrap(err, "failed to check if resource is in-library")
			}
		}
		// Load enrichment data
		d.Movie, _ = models.GetMovieWithMetadataByResourceID(ctx, db, args.ID)
		if d.Movie == nil {
			d.Series, _ = models.GetSeriesWithMetadataByResourceID(ctx, db, args.ID)
		}
		// Load watch history for file list
		if args.User.HasAuth() {
			d.WatchedPaths, _ = models.GetWatchedPaths(ctx, db, args.User.ID, args.ID)
		}
	} else if args.User.HasAuth() {
		return nil, errors.New("failed to connect to database")
	}
	return d, nil
}

func (s *Handler) get(c *gin.Context) {
	getTpl := s.tb.Build("resource/get")
	args, err := s.bindGetArgs(c)
	if err != nil {
		web.RedirectWithErrorAndPath(c, "/", err)
		return
	}
	if !s.useDirectLinks && !s.hasAccessPermission(c, args) {
		// Redirect to homepage instead of showing error
		c.Redirect(http.StatusFound, "/")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	d, err := s.prepareGetData(ctx, args)
	if err != nil {
		_ = c.Error(err)
		web.RedirectWithErrorAndPath(c, "/", err)
		return
	}
	if d == nil {
		web.RedirectWithErrorAndPath(c, "/", errors.New("resource not found"))
		return
	}

	// Set vault availability
	d.Vault = s.vault != nil

	// Prepare initial torrent status (vault DB only, no SSE)
	d.TorrentStatus = s.prepareInitialStatus(ctx, args.ID)

	// Prepare vault button state
	if s.vault != nil && args.User.HasAuth() {
		vaultButton, err := s.prepareVaultButton(ctx, args)
		if err != nil {
			_ = c.Error(errors.Wrap(err, "failed to prepare vault button"))
			getTpl.HTML(http.StatusInternalServerError, web.NewContext(c).WithData(d).WithErr(err))
			return
		}
		d.VaultButton = vaultButton
		if c.Query("pledge-form") == "true" || c.Query("from") == "/vault/pledge/add" {
			vaultPledgeAddForm, err := s.prepareVaultPledgeAddForm(c, args)
			if err != nil {
				_ = c.Error(errors.Wrap(err, "failed to prepare vault form"))
				getTpl.HTML(http.StatusInternalServerError, web.NewContext(c).WithData(d).WithErr(err))
				return
			}

			d.VaultPledgeAddForm = vaultPledgeAddForm
		}
		if c.Query("pledge-remove-form") == "true" || c.Query("from") == "/vault/pledge/remove" {
			vaultPledgeRemoveForm, err := s.prepareVaultPledgeRemoveForm(c, args)
			if err != nil {
				_ = c.Error(errors.Wrap(err, "failed to prepare vault pledge remove form"))
				getTpl.HTML(http.StatusInternalServerError, web.NewContext(c).WithData(d).WithErr(err))
				return
			}
			d.VaultPledgeRemoveForm = vaultPledgeRemoveForm
		}
	}

	// Handle pledge-form parameter

	c.Header("X-Robots-Tag", "noindex, follow")
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

func (s *Handler) resolveFileIdx(ctx context.Context, args *GetArgs, fileIdx int) (string, error) {
	const maxFileIdxIterations = 1000
	listArgs := &api.ListResourceContentArgs{
		Limit:  100,
		Offset: 0,
	}
	var idx int
	var iterations int
	for {
		iterations++
		if iterations > maxFileIdxIterations {
			return "", fmt.Errorf("exceeded max iterations (%d) resolving file index %d", maxFileIdxIterations, fileIdx)
		}
		resp, err := s.api.ListResourceContentCached(ctx, args.Claims, args.ID, listArgs)
		if err != nil {
			return "", errors.Wrap(err, "failed to list resource content for file-idx resolution")
		}
		for _, item := range resp.Items {
			if item.Type == ra.ListTypeFile {
				if idx == fileIdx {
					return item.PathStr, nil
				}
				idx++
			}
		}
		if int(listArgs.Offset)+len(resp.Items) >= resp.Count {
			break
		}
		listArgs.Offset += listArgs.Limit
	}
	return "", fmt.Errorf("file at index %d not found", fileIdx)
}

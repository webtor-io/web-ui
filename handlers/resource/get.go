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
	"github.com/webtor-io/web-ui/services/i18n"
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
	MovieStatus           *models.MovieStatus
	SeriesStatus          *models.SeriesStatus
	PathActions           map[string]*PathAction
	RateForm              *RateForm
	ReleaseSubBanner      *ReleaseSubscribeBanner
	// ResourceMetadata carries per-torrent classification (is_adult /
	// is_sport) and the parsed-name snapshot. Nil when classification
	// hasn't run for this resource yet — templates treat nil as "no
	// flags raised, behave normally".
	ResourceMetadata *models.ResourceMetadata
}

type RateForm struct {
	VideoID       string
	Type          string // "movie" or "series"
	CurrentRating int    // 0 = unrated
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
	if res.MultiFile {
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
	} else {
		// Single-file torrent: rest-api already returned the one file in the
		// resource response, so render it directly without a /list round-trip.
		d.Item = res.File
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
		// Per-torrent classification (is_adult / is_sport). Nil when
		// the resource hasn't been classified yet — template helpers
		// treat that as "no flags" so old resources render normally
		// until the metadata-only backfill catches up.
		d.ResourceMetadata, _ = models.GetResourceMetadataByResourceID(ctx, db, args.ID)
		d.ReleaseSubBanner = prepareReleaseSubscribeBanner(ctx, s.enricher, res, d.Series)
		// Load watch history for file list
		if args.User.HasAuth() {
			d.WatchedPaths, _ = models.GetWatchedPaths(ctx, db, args.User.ID, args.ID)

			// Load IMDB-level user watch status (user_video_status) and augment
			// WatchedPaths for cross-torrent propagation: if this series was
			// watched on a different torrent (different dub/quality), those
			// episodes should still appear as watched in the file list here.
			if d.Movie != nil && d.Movie.MovieMetadata != nil && d.Movie.MovieMetadata.VideoID != "" {
				d.MovieStatus, _ = models.GetMovieStatus(ctx, db, args.User.ID, d.Movie.MovieMetadata.VideoID)
				if d.MovieStatus != nil && d.MovieStatus.Watched && d.Movie.Path != nil {
					if d.WatchedPaths == nil {
						d.WatchedPaths = map[string]bool{}
					}
					d.WatchedPaths[*d.Movie.Path] = true
				}
			}
			if d.Series != nil && d.Series.SeriesMetadata != nil && d.Series.SeriesMetadata.VideoID != "" {
				videoID := d.Series.SeriesMetadata.VideoID
				d.SeriesStatus, _ = models.GetSeriesStatus(ctx, db, args.User.ID, videoID)
				seriesWatched := d.SeriesStatus != nil && d.SeriesStatus.Watched

				epMap, _ := models.GetEpisodeStatusMapForSeries(ctx, db, args.User.ID, videoID)

				// Augment WatchedPaths with cross-torrent propagation: episodes
				// marked watched (via any source — manual, 90%, or series-level)
				// should render their green checkmark in the file browser even
				// when the current playback was on a different torrent.
				if seriesWatched {
					if d.WatchedPaths == nil {
						d.WatchedPaths = map[string]bool{}
					}
					for _, ep := range d.Series.Episodes {
						if ep.Path != nil {
							d.WatchedPaths[*ep.Path] = true
						}
					}
				} else if len(epMap) > 0 {
					if d.WatchedPaths == nil {
						d.WatchedPaths = map[string]bool{}
					}
					for _, ep := range d.Series.Episodes {
						if ep.Path == nil || ep.Season == nil || ep.Episode == nil {
							continue
						}
						key := models.EpisodeKey{Season: *ep.Season, Episode: *ep.Episode}
						if st, ok := epMap[key]; ok && st.Watched {
							d.WatchedPaths[*ep.Path] = true
						}
					}
				}
			}

			// Build path → mark/unmark URL map for the inline watched toggle
			// in the file browser. Covers both movies and series episodes;
			// files that aren't enriched (subtitles, samples, NFOs) get no
			// entry and render without a toggle.
			d.PathActions = buildPathActions(d.Movie, d.Series)
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

	// Localize content metadata (title, plot) to the user's language
	if s.enricher != nil {
		lang := i18n.GetLang(c)
		if d.Movie != nil {
			s.enricher.Localize(ctx, d.Movie.GetMetadata(), lang)
		}
		if d.Series != nil {
			s.enricher.Localize(ctx, d.Series.GetMetadata(), lang)
		}
	}

	// Set vault availability
	d.Vault = s.vault != nil

	// Prepare initial torrent status (vault DB only, no SSE)
	d.TorrentStatus = s.prepareInitialStatus(ctx, args.ID)

	// Prepare vault button state. The vault button is a non-critical UI
	// element (and is also re-fetched async), so if its state can't be
	// resolved — e.g. a transient vault failure or a canceled request — log
	// and render the page without it rather than 500'ing the whole page.
	if s.vault != nil && args.User.HasAuth() {
		vaultButton, err := s.prepareVaultButton(ctx, args)
		if err != nil {
			_ = c.Error(errors.Wrap(err, "failed to prepare vault button"))
		} else {
			d.VaultButton = vaultButton
			if c.Query("pledge-form") == "true" || c.Query("from") == "/vault/add" {
				vaultPledgeAddForm, err := s.prepareVaultPledgeAddForm(c, args)
				if err != nil {
					_ = c.Error(errors.Wrap(err, "failed to prepare vault form"))
					getTpl.HTML(http.StatusInternalServerError, web.NewContext(c).WithData(d).WithErr(err))
					return
				}

				d.VaultPledgeAddForm = vaultPledgeAddForm
			}
			if c.Query("pledge-remove-form") == "true" || c.Query("from") == "/vault/remove" {
				vaultPledgeRemoveForm, err := s.prepareVaultPledgeRemoveForm(c, args)
				if err != nil {
					_ = c.Error(errors.Wrap(err, "failed to prepare vault pledge remove form"))
					getTpl.HTML(http.StatusInternalServerError, web.NewContext(c).WithData(d).WithErr(err))
					return
				}
				d.VaultPledgeRemoveForm = vaultPledgeRemoveForm
			}
		}
	}

	// Prepare rate modal if requested (via rate button or after marking watched).
	if args.User.HasAuth() && (d.Movie != nil || d.Series != nil) {
		if c.Query("rate-form") == "true" {
			d.RateForm = s.prepareRateForm(d)
		}
	}

	getTpl.HTML(http.StatusOK, web.NewContext(c).WithData(d))
}

func (s *Handler) prepareRateForm(d *GetData) *RateForm {
	form := &RateForm{}
	if d.Movie != nil && d.Movie.MovieMetadata != nil && d.Movie.MovieMetadata.VideoID != "" {
		form.VideoID = d.Movie.MovieMetadata.VideoID
		form.Type = "movie"
		if d.MovieStatus != nil && d.MovieStatus.Rating != nil {
			form.CurrentRating = int(*d.MovieStatus.Rating)
		}
	} else if d.Series != nil && d.Series.SeriesMetadata != nil && d.Series.SeriesMetadata.VideoID != "" {
		form.VideoID = d.Series.SeriesMetadata.VideoID
		form.Type = "series"
		if d.SeriesStatus != nil && d.SeriesStatus.Rating != nil {
			form.CurrentRating = int(*d.SeriesStatus.Rating)
		}
	}
	if form.VideoID == "" {
		return nil
	}
	return form
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
	// rest-api accepts a numeric content_id as a torrent-natural-order
	// file index (see rest-api commit 1baa13f), so we can grab the file's
	// path in one round-trip instead of paginating /list.
	resp, err := s.api.ExportResourceContent(ctx, args.Claims, args.ID, strconv.Itoa(fileIdx), "")
	if err != nil {
		return "", errors.Wrap(err, "failed to export resource content for file-idx resolution")
	}
	if resp == nil || resp.Source.PathStr == "" {
		return "", fmt.Errorf("file at index %d not found", fileIdx)
	}
	return resp.Source.PathStr, nil
}

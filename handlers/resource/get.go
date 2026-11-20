package resource

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/webtor-io/web-ui/handlers/common"
	"github.com/webtor-io/web-ui/handlers/index"
	"github.com/webtor-io/web-ui/helpers"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	sv "github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/enrich"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/statusview"
	"github.com/webtor-io/web-ui/services/uploads"
	"github.com/webtor-io/web-ui/services/web"

	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
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
	// FromTool is the tool page the visitor submitted this torrent on
	// (?tool=<slug>, added to the load job's redirect by the log host in
	// partials/load/progress). nil for any other arrival and for a slug that
	// is not in common.Tools. It only adds a line and analytics props to the
	// page header — never a canonical, a robots header or a link.
	FromTool *common.Tool
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
		FromTool: common.ToolByURL(c.Query("tool")),
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
	// StatusView is the transfer status as the page first renders it
	// (services/statusview): the Vault database's word only, the viewer's
	// own link not drawn — the status stream (?session=1) sends the same
	// shape a second later. Render with {{ .StatusView | json }} to seed the
	// client, or read its fields directly.
	StatusView       *statusview.View
	Movie            *models.Movie
	Series           *models.Series
	WatchedPaths     map[string]bool
	MovieStatus      *models.MovieStatus
	SeriesStatus     *models.SeriesStatus
	PathActions      map[string]*PathAction
	RateForm         *RateForm
	ReleaseSubBanner *ReleaseSubscribeBanner
	// ResourceMetadata carries per-torrent classification (is_adult /
	// is_sport) and the parsed-name snapshot. Nil when classification
	// hasn't run for this resource yet — templates treat nil as "no
	// flags raised, behave normally".
	ResourceMetadata *models.ResourceMetadata
	// Credits are the title's directors / creators / cast; nil for anonymous
	// visitors and adult torrents (see get).
	Credits *enrich.Credits
	// adultUnknown is set when the resource_metadata read failed. Unlike a
	// resource that was never classified, its adult flag is then unknown.
	adultUnknown bool
	// Upload is the owner's view of their upload; nil on any other page.
	Upload *UploadView
}

// UploadView is what the owner's page of an upload adds to a torrent's.
type UploadView struct {
	// IsOwner: rest-api showed the upload, and it shows one only to its owner.
	IsOwner bool
	// CanWrite: no vault.resource row, so files can be added and deleted.
	CanWrite bool
	// Name is the upload's root folder, for files added to it; empty while the
	// upload has no name yet.
	Name string
	// Saved: the owner's pledge is there and funded, a copy is kept.
	Saved bool
	// Incomplete: writable, and no file is done yet or a declared part is not.
	Incomplete bool
	// VaultUnknown: Vault is configured and could not be read, so CanWrite and
	// Saved are false for want of an answer, not because the answer is no.
	VaultUnknown bool
	// UnpaidPledge: the owner's own pledge of the upload is there and not
	// funded. Read only when the upload is neither writable nor saved.
	UnpaidPledge bool
}

// uploadView is the owner's view of the upload on d: rest-api showed it, so
// the viewer owns it. A Vault read that failed is an error on c and leaves
// the view unknown, not "not in Vault".
func (s *Handler) uploadView(ctx context.Context, c *gin.Context, d *GetData) *UploadView {
	u := &UploadView{IsOwner: true, CanWrite: true, Name: d.Resource.Name}
	if u.Name == "" {
		if db := s.pg.Get(); db != nil {
			if r, err := models.GetResourceByID(ctx, db, d.Args.ID); err == nil && r != nil {
				u.Name = r.Name
			}
		}
	}
	if s.vault != nil {
		r, err := s.vault.GetResource(ctx, d.Args.ID)
		if err != nil {
			_ = c.Error(errors.Wrap(err, "failed to get vault resource"))
			u.CanWrite, u.VaultUnknown = false, true
		} else {
			u.CanWrite = r == nil
		}
	}
	res := d.Resource
	u.Incomplete = u.CanWrite && (res.FilesCount == 0 || (res.Reserved != nil && *res.Reserved > res.Size))
	return u
}

// ownUnpaidPledge says the viewer has a pledge of the resource and it is not
// funded.
func (s *Handler) ownUnpaidPledge(ctx context.Context, args *GetArgs) (bool, error) {
	resource, err := s.vault.GetResource(ctx, args.ID)
	if err != nil || resource == nil {
		return false, err
	}
	pledge, err := s.vault.GetPledge(ctx, args.User, resource)
	if err != nil {
		return false, err
	}
	return pledge != nil && !pledge.Funded, nil
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
		// A directory holding one entry collapses into it -- except under
		// ?file=: those links reload only #list (partials/list.html), and
		// without a listing that block renders empty.
		if len(list.Items) == 1 && (d.Item == nil || args.File != "") {
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
		// Only the owner gets this far on an upload: rest-api refused
		// anyone else above.
		if uploads.Owned(args.ID) && args.User.HasAuth() {
			s.keepUpload(ctx, db, args, res)
		}
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
		var rmErr error
		d.ResourceMetadata, rmErr = models.GetResourceMetadataByResourceID(ctx, db, args.ID)
		d.adultUnknown = rmErr != nil
		d.ReleaseSubBanner = prepareReleaseSubscribeBanner(ctx, s.enricher, pgBannerSubs{db: db}, args.User, d.Series)
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

var uploadEnrichWait = 3 * time.Second     // how long the owner's page waits for a fresh enrichment
var uploadEnrichTimeout = 30 * time.Minute // the enrichment's own deadline, as jobs.Enrich has

// keepUpload keeps the library row of the owner's upload and, when its files changed since the last
// enrichment, erases its pictures and enriches it again. No failure breaks the page.
func (s *Handler) keepUpload(ctx context.Context, db *pg.DB, args *GetArgs, res *ra.ResourceResponse) {
	if err := models.AddUploadToLibrary(ctx, db, args.User.ID, args.ID, res.Name, res.Size, res.FilesCount); err != nil {
		log.WithError(err).WithField("resource_id", args.ID).Warn("failed to keep the upload's library row")
		return
	}
	if s.enricher == nil || !s.enricher.HasMappers() {
		return
	}
	kept, err := models.DropStaleMediaInfo(ctx, db, args.ID)
	if err != nil {
		log.WithError(err).WithField("resource_id", args.ID).Warn("failed to drop the upload's stale enrichment")
		return
	}
	if kept {
		return
	}
	// The files changed since the pictures were made: a frame may show a file that is no longer there.
	if s.pics != nil {
		if err := s.pics.Erase(ctx, args.ID); err != nil {
			log.WithError(err).WithField("resource_id", args.ID).Warn("failed to erase the upload's stale pictures")
		}
	}
	done := make(chan struct{})
	go func() {
		// A panic here would pass gin's recovery and take the process down;
		// the files' names are the user's.
		defer func() {
			if r := recover(); r != nil {
				log.WithField("stack", string(debug.Stack())).Errorf("upload enrichment panic: %v", r)
			}
			close(done)
		}()
		// Not the request's context: a run cut at the page's deadline would
		// leave the row Processing, and no later call takes such a row.
		ectx, cancel := context.WithTimeout(context.Background(), uploadEnrichTimeout)
		defer cancel()
		if err := s.enricher.Enrich(ectx, args.ID, false, ""); err != nil {
			log.WithError(err).WithField("resource_id", args.ID).Warn("failed to enrich the upload")
		}
	}()
	select {
	case <-done:
	case <-time.After(uploadEnrichWait):
	case <-ctx.Done():
	}
}

// creditsTarget names the title whose directors and cast the page shows:
// none for anonymous visitors (owner, 2026-10-23), and none on an adult
// torrent, or one whose classification could not be read, where a wrong
// match would put real people's names on it.
func creditsTarget(d *GetData, u *auth.User) (string, models.ContentType, bool) {
	ct := models.ContentTypeMovie
	if !u.HasAuth() || d.adultUnknown || (d.ResourceMetadata != nil && d.ResourceMetadata.IsAdult) {
		return "", ct, false
	}
	var md *models.VideoMetadata
	if d.Movie != nil {
		md = d.Movie.GetMetadata()
	} else if d.Series != nil {
		md, ct = d.Series.GetMetadata(), models.ContentTypeSeries
	}
	if md == nil || md.VideoID == "" {
		return "", ct, false
	}
	return md.VideoID, ct, true
}

// addTitleText localizes the title's metadata (title, plot) to the page
// language and puts its credits on d, names in that language where known.
func (s *Handler) addTitleText(ctx context.Context, d *GetData, u *auth.User, lang string) {
	if s.enricher == nil {
		return
	}
	if d.Movie != nil {
		s.enricher.Localize(ctx, d.Movie.GetMetadata(), lang)
	}
	if d.Series != nil {
		s.enricher.Localize(ctx, d.Series.GetMetadata(), lang)
	}
	if id, ct, ok := creditsTarget(d, u); ok {
		d.Credits = s.enricher.CachedCredits(ctx, id, ct, lang)
	}
}

func (s *Handler) get(c *gin.Context) {
	getTpl := s.tb.Build("resource/get")
	args, err := s.bindGetArgs(c)
	if err != nil {
		s.notFound(c, err)
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
	var pe *api.PrivateError
	if errors.As(err, &pe) && pe.Status == http.StatusForbidden {
		if pe.Reason == api.ReasonNoRecord && s.uploadGone(ctx, args) {
			s.forgetGone(ctx, args.ID)
			s.notFound(c, web.NewUserError("upload.gone", pe))
			return
		}
		s.privateRefusal(ctx, c, args, pe)
		return
	}
	if err != nil {
		_ = c.Error(err)
		web.RedirectWithErrorAndPath(c, "/", err)
		return
	}
	if d == nil {
		if s.uploadGone(ctx, args) {
			s.forgetGone(ctx, args.ID)
			s.notFound(c, web.NewUserError("upload.gone", errors.New("upload gone")))
			return
		}
		s.notFound(c, errors.New("resource not found"))
		return
	}
	if d.Resource.Private {
		noStoreNoIndex(c)
	}

	s.addTitleText(ctx, d, args.User, i18n.GetLang(c))

	// Set vault availability
	d.Vault = s.vault != nil

	// Prepare initial torrent status (vault DB only, no SSE)
	d.TorrentStatus = s.prepareInitialStatus(ctx, args.ID)
	d.StatusView = s.initialView(c, d.TorrentStatus)

	if uploads.Owned(args.ID) {
		d.Upload = s.uploadView(ctx, c, d)
	}

	// Prepare vault button state. The vault button is a non-critical UI
	// element (and is also re-fetched async), so if its state can't be
	// resolved — e.g. a transient vault failure or a canceled request — log
	// and render the page without it rather than 500'ing the whole page.
	if s.vault != nil && args.User.HasAuth() {
		vaultButton, err := s.prepareVaultButton(ctx, args)
		if err != nil {
			_ = c.Error(errors.Wrap(err, "failed to prepare vault button"))
			if d.Upload != nil {
				d.Upload.VaultUnknown = true
			}
		} else {
			d.VaultButton = vaultButton
			if d.Upload != nil {
				d.Upload.Saved = vaultButton.Funded
				// The button reads no pledge of an unfunded row: the
				// owner's own one is read here.
				if !d.Upload.CanWrite && !d.Upload.Saved {
					unpaid, err := s.ownUnpaidPledge(ctx, args)
					if err != nil {
						_ = c.Error(errors.Wrap(err, "failed to read the viewer's pledge"))
						d.Upload.VaultUnknown = true
					} else {
						d.Upload.UnpaidPledge = unpaid
					}
				}
			}
			if c.Query("pledge-form") == "true" || c.Query("from") == "/vault/add" {
				vaultPledgeAddForm, err := s.prepareVaultPledgeAddForm(c, args, trackerPrivate(d.Resource.ResourceResponse))
				if err != nil {
					_ = c.Error(errors.Wrap(err, "failed to prepare vault form"))
					getTpl.HTML(http.StatusInternalServerError, web.NewContext(c).WithData(d).WithErr(err))
					return
				}
				vaultPledgeAddForm.UploadEmpty, vaultPledgeAddForm.UploadIncomplete = uploadPledgeState(d.Resource.ResourceResponse)

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

// noStoreNoIndex keeps a private torrent's page out of every cache and index
// (Р11): the refusal and the page of whoever may play it alike. The second
// has the torrent's name, and anti-goal 6 forbids serving one person's page
// to another from a cache.
func noStoreNoIndex(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Robots-Tag", "noindex")
}

// privateRefusal answers rest-api's 403 for a private torrent: why, and the
// way out, without the torrent's name (rest-api gave none). A 503 or a 429
// says nothing about this user's right and keeps the redirect above.
func (s *Handler) privateRefusal(ctx context.Context, c *gin.Context, args *GetArgs, pe *api.PrivateError) {
	noStoreNoIndex(c)
	r := web.NewPrivateRefusal(pe, args.User.HasAuth(), args.ID)
	log.WithError(pe).
		WithField("err_key", r.Key).
		WithField("surface", "page").
		WithField("path", helpers.RedactURL(c.Request.URL.Path)).
		Info("user error shown")
	if web.WantsJSON(c) {
		c.JSON(http.StatusForbidden, gin.H{"status": "error", "message": r.Key})
		return
	}
	r.Target = "main"
	// An upload's Vault copy plays for its owner alone, who is not refused.
	r.VaultKept = args.User.HasAuth() && pe.Reason == api.ReasonNoRecord && !uploads.Owned(args.ID) && s.vaulted(ctx, c, args.ID)
	s.tb.Build("resource/private").HTML(http.StatusForbidden, web.NewContext(c).WithData(r))
}

// trackerPrivate says the resource is a private torrent, whose pledge modal
// warns about the viewer's tracker. An upload is private and has no tracker.
func trackerPrivate(r *ra.ResourceResponse) bool {
	return r.Private && !uploads.Owned(r.ID)
}

// uploadGone says the viewer owns the upload rest-api did not answer for: it
// is one of theirs, and their library row of it is there. No database, or a
// failed read, says no.
func (s *Handler) uploadGone(ctx context.Context, args *GetArgs) bool {
	if !uploads.Owned(args.ID) || !args.User.HasAuth() {
		return false
	}
	db := s.pg.Get()
	if db == nil {
		return false
	}
	lib, err := models.GetLibraryByResourceID(ctx, db, args.User.ID, args.ID)
	if err != nil {
		log.WithError(err).WithField("resource_id", args.ID).Warn("failed to read the owner's library row of an upload")
		return false
	}
	return lib != nil
}

// forgetGone erases what web-ui derived from the owner's upload that is gone:
// its enrichment and pictures, which the next page view brings back if the
// upload is there after all. Not the watch history or the subtitle bindings:
// rest-api's 404 or no_record is wrong while the upload's home node is away.
// The page renders whatever this does.
func (s *Handler) forgetGone(ctx context.Context, id string) {
	if err := uploads.Forget(ctx, s.pg.Get(), s.pics, id, false); err != nil {
		log.WithError(err).WithField("resource_id", id).Warn("failed to forget an upload that is gone")
	}
}

// vaulted says Vault keeps a copy of the torrent. Unknown (no Vault, or its
// database did not answer) reads as no: the line is a promise.
func (s *Handler) vaulted(ctx context.Context, c *gin.Context, id string) bool {
	if s.vault == nil {
		return false
	}
	res, err := s.vault.GetResource(ctx, id)
	if err != nil {
		_ = c.Error(errors.Wrap(err, "failed to get vault resource"))
		return false
	}
	return res != nil && res.Vaulted
}

// notFound answers a resource URL that names nothing: the ID is not one at
// all (/wp-login.php, /library, an apple-touch-icon probe -- anything without
// a run of five hex digits, error.invalid_resource), or rest-api has no
// torrent under it (a share link whose torrent is gone, a mistyped hash,
// error.not_found). Its owner's upload that rest-api no longer has -- a 404,
// or a 403 no_record -- is the second case with its own text, upload.gone
// (uploadGone).
//
// Both used to redirect to /?err=, so every such URL answered with a 200 home
// page and none of the stale hash links in search indexes could ever leave
// them. Now the URL itself answers 404 with that same page -- the reason above
// the form -- so a person still gets the message and the form while a crawler
// gets a status it can act on. The X-Robots-Tag is the route's default
// "noindex, follow" (web.NoindexDefault; resource pages are not in the
// sitemap), which is why nothing here sets one.
//
// Only these outcomes come here. A refused resource (banned or
// stoplisted: rest-api answers 403, error.forbidden) keeps its redirect -- it
// has its own policy -- and so does a failure on our side (rest-api or the
// database did not answer), which says nothing about whether the URL is dead.
// A banned hash whose torrent has already left the store is indistinguishable
// from an unknown one: rest-api answers 404 for both, and so it lands here.
//
// The page is the home page of whoever asked, signed-in navbar and continue-
// watching row included, so it is private, no-store: some of these URLs end
// in a static extension (/apple-touch-icon-precomposed.png), and an edge
// cache that keeps 404s for those would hand one person's page to the next.
// The redirect this replaced carried nothing personal.
func (s *Handler) notFound(c *gin.Context, err error) {
	c.Header("Cache-Control", "private, no-store")
	key := web.ClassifyError(err)
	log.WithError(err).
		WithField("err_key", key).
		WithField("surface", "page").
		WithField("path", c.Request.URL.Path).
		Info("user error shown")
	if web.WantsJSON(c) {
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": key})
		return
	}
	// The home page's own renderer: the "index" view is registered by
	// handlers/index, which this handler's POST already renders too.
	index.Render(c, s.tb, s.pg, http.StatusNotFound, &index.Data{}, key, nil)
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

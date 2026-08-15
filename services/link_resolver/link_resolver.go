package link_resolver

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/lazymap"
	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/models"
	vmodels "github.com/webtor-io/web-ui/models/vault"
	"github.com/webtor-io/web-ui/services/api"
	ci "github.com/webtor-io/web-ui/services/cache_index"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/link_resolver/backends"
	co "github.com/webtor-io/web-ui/services/link_resolver/common"
)

// LinkResolver resolves streaming links across multiple backends (RealDebrid, Torbox, Webtor)
// by checking content availability and generating direct download URLs
type LinkResolver struct {
	pg                   *cs.PG
	api                  *api.Api
	cacheIndex           *ci.CacheIndex
	userBackends         map[models.StreamingBackendType]co.Backend
	webtorBackend        *backends.Webtor
	enabledBackendsCache *lazymap.LazyMap[[]*models.StreamingBackend]
}

// New creates a new LinkResolver with configured backends
func New(cl *http.Client, pg *cs.PG, apiService *api.Api, cacheIndex *ci.CacheIndex) *LinkResolver {
	return &LinkResolver{
		pg:         pg,
		api:        apiService,
		cacheIndex: cacheIndex,
		userBackends: map[models.StreamingBackendType]co.Backend{
			models.StreamingBackendTypeRealDebrid: backends.NewRealDebrid(cl),
			models.StreamingBackendTypeTorbox:     backends.NewTorbox(cl),
		},
		webtorBackend: backends.NewWebtor(apiService),
		enabledBackendsCache: lazymap.New[[]*models.StreamingBackend](&lazymap.Config{
			Expire:      1 * time.Minute,
			ErrorExpire: 30 * time.Second,
		}),
	}
}

func (s *LinkResolver) GetUserEnabledBackends(ctx context.Context, userID uuid.UUID) ([]*models.StreamingBackend, error) {
	return s.enabledBackendsCache.Get(userID.String(), func() ([]*models.StreamingBackend, error) {
		return s.getUserEnabledBackends(ctx, userID)
	})
}

func (s *LinkResolver) getUserEnabledBackends(ctx context.Context, userID uuid.UUID) ([]*models.StreamingBackend, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database not initialized")
	}
	// Get user's streaming backends ordered by priority (highest first)
	userBackends, err := models.GetUserStreamingBackends(ctx, db, userID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get user streaming backends")
	}

	// Filter to only enabled backends
	enabledBackends := make([]*models.StreamingBackend, 0)
	for _, backend := range userBackends {
		if _, ok := s.userBackends[backend.Type]; !ok {
			log.WithField("backend_type", backend.Type).Warn("backend implementation not found")
			continue
		}
		if backend.Enabled {
			enabledBackends = append(enabledBackends, backend)
		}
	}
	log.WithField("enabled_backends_count", len(enabledBackends)).Debug("found enabled streaming backends")
	return enabledBackends, nil
}

// ResolveLink resolves a streaming link for the file at (hash, fileIdx).
// All backends speak fileIdx directly: Webtor passes it through as a
// numeric content_id to rest-api, RD/Torbox use it as the index into
// their own torrent.Files slice. No path lookup is needed anywhere.
// Returns nil if content is not available or user doesn't have access.
func (s *LinkResolver) ResolveLink(ctx context.Context, userID uuid.UUID, apiClaims *api.Claims, userClaims *claims.Data, hash string, fileIdx int, requiresPayment bool) (*co.LinkResult, error) {
	log.WithFields(log.Fields{
		"hash":             hash,
		"file_idx":         fileIdx,
		"requires_payment": requiresPayment,
	}).Debug("resolving link")
	enabledBackends, err := s.GetUserEnabledBackends(ctx, userID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to load user enabled backends")
	}

	for _, userBackend := range enabledBackends {
		backend, ok := s.userBackends[userBackend.Type]
		if !ok {
			log.WithField("backend_type", userBackend.Type).Warn("backend implementation not found")
			continue
		}
		url, cached, berr := backend.ResolveLink(ctx, userBackend.AccessToken, hash, fileIdx)
		if berr != nil {
			log.WithError(berr).WithField("backend_type", userBackend.Type).Warn("failed to generate link from backend")
			continue
		}
		if !cached {
			log.WithField("backend_type", userBackend.Type).Warn("link is not cached")
			continue
		}
		if merr := s.cacheIndex.MarkAsCached(ctx, userBackend.Type, hash, fileIdx); merr != nil {
			return nil, errors.Wrap(merr, "failed to mark as cached in cache index")
		}
		log.WithFields(log.Fields{
			"url":          url,
			"cached":       cached,
			"backend_type": userBackend.Type,
		}).Info("generated streaming link from backend")
		return &co.LinkResult{
			URL:         url,
			ServiceType: userBackend.Type,
			Cached:      cached,
		}, nil
	}

	// Fallback to webtor. Free users hit the paywall here.
	if requiresPayment && !s.isPaidUser(userClaims) {
		return nil, nil
	}
	url, cached, err := s.webtorBackend.ResolveLink(ctx, apiClaims, hash, fileIdx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate webtor link")
	}
	if cached {
		if merr := s.cacheIndex.MarkAsCached(ctx, models.StreamingBackendTypeWebtor, hash, fileIdx); merr != nil {
			return nil, errors.Wrap(merr, "failed to mark as cached in cache index")
		}
	}

	log.WithFields(log.Fields{
		"url":          url,
		"cached":       cached,
		"backend_type": "webtor",
	}).Info("generated webtor link")
	return &co.LinkResult{
		URL:         url,
		ServiceType: models.StreamingBackendTypeWebtor,
		Cached:      cached,
	}, nil
}

// listPageSize bounds a single rest-api listing page while looking for the
// file a stream means. rest-api caps a page at 1000 and serves listings from
// the lightweight torrent-store manifest, so one page covers all but the
// largest torrents — same size services/libfs uses, and ~10x fewer round
// trips than a 100-item page on a season pack.
const listPageSize = 1000

// torrentLister is the slice of *api.Api the file picker needs. Narrowed to
// an interface so the selection rules are testable without an HTTP client
// behind them; services/libfs declares the same one for the same reason.
type torrentLister interface {
	ListResourceContentCached(ctx context.Context, c *api.Claims, infohash string, args *api.ListResourceContentArgs) (*ra.ListResponse, error)
}

// PickPrimaryFileIdx returns the natural file index a stream should play when
// its source never said which file it meant.
//
// Torznab indexers are that case, and the only one: a search result names a
// torrent, not a file inside it. Library streams persist file_idx (migration
// 60) and Stremio addons send fileIdx, which is why every other caller can
// pass an index straight through. Defaulting to 0 instead would play whatever
// the torrent happens to list first — a sample, an .nfo, or episode 1 of a
// season pack no matter which episode the user picked.
//
// Selection is the largest video file, which is what a single-episode or
// single-movie release resolves to. A multi-episode pack has no right answer
// at this layer; the largest file at least plays something the user can see
// is wrong, rather than silently starting episode 1.
func (s *LinkResolver) PickPrimaryFileIdx(ctx context.Context, apiClaims *api.Claims, hash string) (int, error) {
	if s.api == nil {
		return 0, errors.New("api service is not configured")
	}
	return pickFileIdx(ctx, s.api, apiClaims, hash, nil)
}

// PickEpisodeFileIdx returns the index of the file holding a given episode,
// falling back to the largest video when the torrent holds no file that
// names it.
//
// This is what makes an indexer usable for series: an episode query answers
// with season packs at least as often as with single episodes — on RU
// trackers that is the normal shape — so "the biggest video" would start
// episode one no matter which episode the user picked.
func (s *LinkResolver) PickEpisodeFileIdx(ctx context.Context, apiClaims *api.Claims, hash string, season, episode int) (int, error) {
	if s.api == nil {
		return 0, errors.New("api service is not configured")
	}
	return pickFileIdx(ctx, s.api, apiClaims, hash, newEpisodeMatcher(season, episode))
}

// pickFileIdx walks a torrent's listing and returns the index of the video
// file to play: the largest one the preferred predicate accepts, or — when
// nothing does — the largest video overall.
//
// The fallback is what makes the predicate safe to be strict: a pack whose
// files are named in a way no pattern covers still plays something.
func pickFileIdx(ctx context.Context, lister torrentLister, apiClaims *api.Claims, hash string, preferred func(ra.ListItem) bool) (int, error) {
	var best, fallback largestVideo
	var offset uint
	for {
		resp, err := lister.ListResourceContentCached(ctx, apiClaims, hash, &api.ListResourceContentArgs{
			Limit:  listPageSize,
			Offset: offset,
		})
		if err != nil {
			return 0, errors.Wrap(err, "failed to list resource content")
		}
		if resp == nil {
			break
		}
		for _, item := range resp.Items {
			if item.MediaFormat != ra.Video {
				continue
			}
			if preferred != nil && preferred(item) {
				best.offer(item)
				continue
			}
			fallback.offer(item)
		}
		// Advance by what the page actually held, not by what was asked
		// for: a server free to return fewer items than the limit would
		// otherwise have whole blocks of files skipped silently. An empty
		// page ends the walk rather than looping on the same offset.
		if len(resp.Items) == 0 || (resp.Count-int(offset)) <= len(resp.Items) {
			break
		}
		offset += uint(len(resp.Items))
	}
	if best.found {
		return best.idx, nil
	}
	if fallback.found {
		return fallback.idx, nil
	}
	return 0, errors.New("torrent has no video file")
}

// largestVideo keeps the biggest item offered to it.
type largestVideo struct {
	idx   int
	size  int64
	found bool
}

func (l *largestVideo) offer(item ra.ListItem) {
	if !l.found || item.Size > l.size {
		l.idx, l.size, l.found = item.Index, item.Size, true
	}
}

// episodeMarkers are the ways a file inside a pack names its episode. The
// first two are the international spellings — S01E05 with any separator, and
// 1x05. The last is the RU one: season packs from RU trackers routinely name
// files "05 серия" with the season only in the directory above, so the
// season number is not required there — the pack is already the season.
//
// %[1]d is the season, %[2]d the episode. The (\D|^) guards keep S01E05 from
// matching S01E050.
var episodeMarkers = []string{
	`s0*%[1]d[\s._-]*e0*%[2]d(\D|$)`,
	`(\D|^)0*%[1]dx0*%[2]d(\D|$)`,
	`(\D|^)0*%[2]d\s*(?:-?я\s*)?(?:сери[яйи]|эпизод)`,
	`(?:сери[яйи]|эпизод)[\s._-]*0*%[2]d(\D|$)`,
}

// newEpisodeMatcher compiles the episode patterns once for a whole listing
// walk. Compiling them per file cost a torrent-sized number of compilations
// on every playback click.
func newEpisodeMatcher(season, episode int) func(ra.ListItem) bool {
	if season <= 0 || episode <= 0 {
		return nil
	}
	res := make([]*regexp.Regexp, 0, len(episodeMarkers))
	for _, tpl := range episodeMarkers {
		re, err := regexp.Compile(`(?i)` + fmt.Sprintf(tpl, season, episode))
		if err != nil {
			continue
		}
		res = append(res, re)
	}
	return func(item ra.ListItem) bool {
		name := strings.ToLower(item.PathStr)
		for _, re := range res {
			if re.MatchString(name) {
				return true
			}
		}
		return false
	}
}


// CheckTorrentAvailability answers, for a stream whose file is not known yet
// (see stremio.StreamItem.FileIdxUnknown), the two things that do not depend
// on which file it is: whether this user gets a Webtor-served link at all,
// and whether the torrent is already in hot storage.
//
// It deliberately skips the per-file cache index. A lookup keyed on a file we
// have not picked would describe a different file — but skipping the check
// entirely, as this used to, is worse: a nil result reads as "no backend will
// serve this" and labels the stream P2P, which is exactly what a paid user
// seeing their own indexer's releases must not be told.
func (s *LinkResolver) CheckTorrentAvailability(ctx context.Context, cla *claims.Data, hash string, requiresPayment bool) (*co.CheckAvailabilityResult, error) {
	if requiresPayment && !s.isPaidUser(cla) {
		return nil, nil
	}
	cached := false
	if db := s.pg.Get(); db != nil {
		res, err := vmodels.GetResource(ctx, db, hash)
		if err != nil {
			log.WithError(err).WithField("hash", hash).Debug("vault resource lookup failed")
		} else if res != nil && res.Vaulted {
			cached = true
		}
	}
	return &co.CheckAvailabilityResult{
		Cached:      cached,
		ServiceType: models.StreamingBackendTypeWebtor,
	}, nil
}

// isPaidUser checks if the user has paid tier
func (s *LinkResolver) isPaidUser(userClaims *claims.Data) bool {
	if userClaims == nil || userClaims.Context == nil || userClaims.Context.Tier == nil {
		return true
	}
	return userClaims.Context.Tier.Id > 0
}

// CheckAvailability reports whether the file (hash, fileIdx) is streamable
// on any of the user's enabled backends, falling back to Webtor.
// fileIdx is always known at the call site — Stremio addons and Library
// streams both populate StreamItem.FileIdx — which lets us skip the
// rest-api ListResourceContent round-trip that path-based resolution
// previously required.
func (s *LinkResolver) CheckAvailability(ctx context.Context, id uuid.UUID, cla *claims.Data, hash string, fileIdx int, requiresPayment bool) (*co.CheckAvailabilityResult, error) {
	r, err := s.cacheIndex.IsCached(ctx, hash, fileIdx)
	if err != nil {
		return nil, err
	}
	eb, err := s.GetUserEnabledBackends(ctx, id)
	if err != nil {
		return nil, err
	}
	var (
		cached bool
		bt     models.StreamingBackendType
	)
	for _, userBackend := range eb {
		for _, cir := range r {
			if cir.BackendType == userBackend.Type {
				cached = true
				bt = cir.BackendType
				break
			}
		}
	}
	if cached {
		return &co.CheckAvailabilityResult{
			Cached:      true,
			ServiceType: bt,
		}, nil
	}
	if requiresPayment && !s.isPaidUser(cla) {
		return nil, nil
	}
	for _, cir := range r {
		if cir.BackendType == models.StreamingBackendTypeWebtor {
			cached = true
			break
		}
	}
	// Fallback: a resource that is vaulted (vault.resource.vaulted=true) is
	// guaranteed to be in Webtor's hot storage. The cacheIndex only learns
	// about this after a play has gone through ResolveLink, so a freshly
	// vaulted file would otherwise miss the ⚡ marker until first stream.
	// One indexed row read on vault.resource closes that gap cheaply.
	if !cached {
		if db := s.pg.Get(); db != nil {
			res, verr := vmodels.GetResource(ctx, db, hash)
			if verr != nil {
				log.WithError(verr).WithField("hash", hash).Debug("vault resource lookup failed")
			} else if res != nil && res.Vaulted {
				cached = true
				if merr := s.cacheIndex.MarkAsCached(ctx, models.StreamingBackendTypeWebtor, hash, fileIdx); merr != nil {
					log.WithError(merr).WithField("hash", hash).Debug("failed to mark vaulted resource as cached")
				}
			}
		}
	}
	return &co.CheckAvailabilityResult{
		Cached:      cached,
		ServiceType: models.StreamingBackendTypeWebtor,
	}, nil
}

func (s *LinkResolver) Validate(ctx context.Context, backend *models.StreamingBackend) error {
	if _, ok := s.userBackends[backend.Type]; !ok {
		return errors.New("backend implementation not found")
	}
	return s.userBackends[backend.Type].Validate(ctx, backend.AccessToken)
}

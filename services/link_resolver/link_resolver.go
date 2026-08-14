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
// file a stream means.
const listPageSize = 100

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
	var (
		bestIdx  int
		bestSize int64
		found    bool
		offset   uint
	)
	for {
		resp, err := s.api.ListResourceContentCached(ctx, apiClaims, hash, &api.ListResourceContentArgs{
			Limit:  listPageSize,
			Offset: offset,
		})
		if err != nil {
			return 0, errors.Wrap(err, "failed to list resource content")
		}
		if resp == nil {
			break
		}
		if idx, size, ok := pickLargestVideo(resp.Items); ok && (!found || size > bestSize) {
			bestIdx, bestSize, found = idx, size, true
		}
		if (resp.Count - int(offset)) <= len(resp.Items) {
			break
		}
		offset += listPageSize
	}
	if !found {
		return 0, errors.New("torrent has no video file")
	}
	return bestIdx, nil
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
	var (
		bestIdx  int
		bestSize int64
		found    bool
		fbIdx    int
		fbSize   int64
		fbFound  bool
		offset   uint
	)
	for {
		resp, err := s.api.ListResourceContentCached(ctx, apiClaims, hash, &api.ListResourceContentArgs{
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
			if matchesEpisode(item.PathStr, season, episode) {
				if !found || item.Size > bestSize {
					bestIdx, bestSize, found = item.Index, item.Size, true
				}
				continue
			}
			if !fbFound || item.Size > fbSize {
				fbIdx, fbSize, fbFound = item.Index, item.Size, true
			}
		}
		if (resp.Count - int(offset)) <= len(resp.Items) {
			break
		}
		offset += listPageSize
	}
	if found {
		return bestIdx, nil
	}
	if fbFound {
		return fbIdx, nil
	}
	return 0, errors.New("torrent has no video file")
}

// matchesEpisode reports whether a file path names the given episode. The
// patterns are the two spellings that actually appear in release names —
// S01E05 (with any separator) and 1x05 — anchored on non-digits so S01E05
// does not also match S01E050.
func matchesEpisode(path string, season, episode int) bool {
	if season <= 0 || episode <= 0 {
		return false
	}
	name := strings.ToLower(path)
	for _, tpl := range []string{
		fmt.Sprintf(`s0*%d[\s._-]*e0*%d(\D|$)`, season, episode),
		fmt.Sprintf(`(\D|^)0*%dx0*%d(\D|$)`, season, episode),
	} {
		re, err := regexp.Compile(`(?i)` + tpl)
		if err != nil {
			continue
		}
		if re.MatchString(name) {
			return true
		}
	}
	return false
}

// pickLargestVideo returns the index and size of the largest video file in a
// listing page, and whether the page held one at all.
func pickLargestVideo(items []ra.ListItem) (int, int64, bool) {
	var (
		bestIdx  int
		bestSize int64
		found    bool
	)
	for _, item := range items {
		if item.MediaFormat != ra.Video {
			continue
		}
		if !found || item.Size > bestSize {
			bestIdx, bestSize, found = item.Index, item.Size, true
		}
	}
	return bestIdx, bestSize, found
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

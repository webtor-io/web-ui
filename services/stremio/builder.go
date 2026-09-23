package stremio

import (
	"context"
	"net/http"
	"time"

	"github.com/pkg/errors"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/lazymap"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/common"
	lr "github.com/webtor-io/web-ui/services/link_resolver"
	rum "github.com/webtor-io/web-ui/services/request_url_mapper"
	tn "github.com/webtor-io/web-ui/services/torznab"
)

type Builder struct {
	pg               *cs.PG
	cache            *lazymap.LazyMap[*StreamsResponse]
	domain           string
	rapi             *api.Api
	cl               *http.Client
	userAgent        string
	secret           string
	requestURLMapper *rum.RequestURLMapper
	tn               *tn.Client
	titles           tn.TitleResolver
	tnCache          *lazymap.LazyMap[*StreamsResponse]
}

func NewBuilder(c *cli.Context, pg *cs.PG, cl *http.Client, rapi *api.Api, requestURLMapper *rum.RequestURLMapper, tnClient *tn.Client, titles tn.TitleResolver) *Builder {
	return &Builder{
		pg: pg,
		cache: lazymap.New[*StreamsResponse](&lazymap.Config{
			Expire:      1 * time.Minute,
			ErrorExpire: 10 * time.Second,
		}),
		domain:           c.String(common.DomainFlag),
		secret:           c.String(common.SessionSecretFlag),
		rapi:             rapi,
		cl:               cl,
		userAgent:        c.String(StremioUserAgentFlag),
		requestURLMapper: requestURLMapper,
		tn:               tnClient,
		titles:           titles,
		// Indexers get their own map. lazymap serialises work per map with
		// a default concurrency of 10, and a Torznab fetch occupies its
		// slot for up to 12s against an addon's 5s — sharing one map lets
		// a handful of slow indexers starve every addon fetch on the
		// replica. Concurrency is raised for the same reason.
		tnCache: lazymap.New[*StreamsResponse](&lazymap.Config{
			Concurrency: 32,
			Expire:      1 * time.Minute,
			ErrorExpire: 10 * time.Second,
		}),
	}
}

func (s *Builder) BuildManifestService(u *auth.User, hasToken bool) (ManifestService, error) {
	return NewManifest(s.domain, u, hasToken), nil
}

func (s *Builder) BuildCatalogService(u *auth.User) (CatalogService, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database not initialized")
	}
	cas := NewLibrary(s.domain, db, u, s.rapi, nil)

	return cas, nil
}

func (s *Builder) BuildMetaService(u *auth.User) (MetaService, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database not initialized")
	}
	mes := NewLibrary(s.domain, db, u, s.rapi, nil)

	return mes, nil
}

// BuildTorznabStreamsService builds only the Torznab half of the stream
// pipeline. Discover needs it because the browser cannot query an indexer
// itself: Jackett and Prowlarr send no CORS headers, and a self-hosted
// indexer on http:// is blocked as mixed content from an https:// page
// regardless. Everything else Discover does still happens client-side.
//
// Returns nil when Torznab is not configured, so callers can skip the
// endpoint entirely.
func (s *Builder) BuildTorznabStreamsService(ctx context.Context, u *auth.User) (StreamsService, error) {
	if s.tn == nil {
		return nil, nil
	}
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database not initialized")
	}
	return NewTorznabCompositeStreamsByUserID(ctx, db, s.tn, u.ID, s.titles, s.tnCache)
}

// BuildPollStreamsService builds the pipeline the release-subscription
// poller runs: the user's own addons and indexers, deduplicated by infohash.
//
// It stops where the interactive pipeline continues. Library is left out —
// the poller asks "has anything new appeared", and what the user already has
// in their Vault is by definition not new. EnrichStream is left out because
// everything it adds (playback tokens, availability marks, /stremio/resolve
// URLs) exists to make a stream clickable, and a cron job clicks nothing; it
// also needs a request-scoped token this job has no way to mint.
//
// The language filter is left out too, and that one is not an omission: a
// subscription carries its own language and resolution preferences (copied
// from the account when it was created, editable in the profile since), and
// the poller applies those. Filtering here as well would apply the account's
// current setting on top of the subscription's own.
//
// What stays is dedup, which gives one row per infohash — the key a
// subscription remembers releases by.
func (s *Builder) BuildPollStreamsService(ctx context.Context, u *auth.User) (StreamsService, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database not initialized")
	}

	// DiscoverOnly is deliberately not honoured here. It answers "where do
	// my addon results show up" — Discover on the web, and not in the
	// Stremio app's stream list — not "may Webtor query my sources at all".
	// A subscription is the user asking us to query them; skipping would
	// hand a DiscoverOnly user a subscription that can never fire.
	services := []StreamsService{}

	acs, err := NewAddonCompositeStreamsByUserID(ctx, db, s.cl, u.ID, s.cache, s.userAgent, s.requestURLMapper)
	if err != nil {
		return nil, err
	}
	services = append(services, acs)

	if s.tn != nil {
		tcs, err := NewTorznabCompositeStreamsByUserID(ctx, db, s.tn, u.ID, s.titles, s.tnCache)
		if err != nil {
			return nil, err
		}
		services = append(services, tcs)
	}

	cs := NewCompositeStream(services)
	return NewDedupStream(cs), nil
}

func (s *Builder) BuildStreamsService(ctx context.Context, u *auth.User, lr *lr.LinkResolver, apiClaims *api.Claims, cla *claims.Data, token string) (StreamsService, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database not initialized")
	}

	settings, err := GetUserSettingsDataByClaims(ctx, db, u.ID)
	if err != nil {
		return nil, err
	}

	services := []StreamsService{NewLibrary(s.domain, db, u, s.rapi, apiClaims)}

	if !settings.DiscoverOnly {
		acs, err := NewAddonCompositeStreamsByUserID(ctx, db, s.cl, u.ID, s.cache, s.userAgent, s.requestURLMapper)
		if err != nil {
			return nil, err
		}
		services = append(services, acs)

		// Torznab indexers come after the addons on purpose: DedupStream
		// keeps the first stream per infohash, and an addon result carries
		// a file index the indexer has no way of knowing.
		if s.tn != nil {
			tcs, err := NewTorznabCompositeStreamsByUserID(ctx, db, s.tn, u.ID, s.titles, s.tnCache)
			if err != nil {
				return nil, err
			}
			services = append(services, tcs)
		}
	}

	cs := NewCompositeStream(services)
	ds := NewDedupStream(cs)
	ps := NewPreferredStream(ds, db, u, cla)
	lfs := NewLangFilterStream(ps, db, u)
	es := NewEnrichStream(lfs, lr, u, cla, s.domain, token, s.secret)

	return es, nil
}

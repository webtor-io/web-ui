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
	ci "github.com/webtor-io/web-ui/services/cache_index"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/common"
	lr "github.com/webtor-io/web-ui/services/link_resolver"
)

type Builder struct {
	pg            *cs.PG
	cache         lazymap.LazyMap[*StreamsResponse]
	domain        string
	rapi          *api.Api
	cl            *http.Client
	userAgent     string
	secret        string
	cacheIndex    *ci.CacheIndex
	cacheAddonURL string
}

func NewBuilder(c *cli.Context, pg *cs.PG, cl *http.Client, rapi *api.Api, cacheIndex *ci.CacheIndex) *Builder {
	return &Builder{
		pg: pg,
		cache: lazymap.New[*StreamsResponse](&lazymap.Config{
			Expire:      1 * time.Minute,
			ErrorExpire: 10 * time.Second,
		}),
		domain:        c.String(common.DomainFlag),
		secret:        c.String(common.SessionSecretFlag),
		rapi:          rapi,
		cl:            cl,
		userAgent:     c.String(StremioUserAgentFlag),
		cacheIndex:    cacheIndex,
		cacheAddonURL: c.String(StremioCacheAddonURLFlag),
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

func (s *Builder) BuildStreamsService(ctx context.Context, u *auth.User, lr *lr.LinkResolver, apiClaims *api.Claims, cla *claims.Data, token string) (StreamsService, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database not initialized")
	}
	acs, err := NewAddonCompositeStreamsByUserID(ctx, db, s.cl, u.ID, s.cache, s.userAgent)
	if err != nil {
		return nil, err
	}
	sts := NewLibrary(s.domain, db, u, s.rapi, apiClaims)
	cs := NewCompositeStream([]StreamsService{sts, acs})
	ds := NewDedupStream(cs)
	ps := NewPreferredStream(ds, db, u, cla)
	prs := NewPrefetchResourceStream(ps, s.rapi, apiClaims)
	pcs := NewPrefetchCacheStream(prs, s.cl, s.pg, u, s.cacheIndex, s.cacheAddonURL, s.userAgent, s.rapi, apiClaims)
	es := NewEnrichStream(pcs, s.rapi, lr, u, apiClaims, cla, s.domain, token, s.secret)

	return es, nil
}

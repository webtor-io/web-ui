package stremio

import (
	"net/http"
	"time"

	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/lazymap"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/common"
)

type Builder struct {
	pg     *cs.PG
	cache  lazymap.LazyMap[*StreamsResponse]
	domain string
	rapi   *api.Api
	cl     *http.Client
}

func NewBuilder(c *cli.Context, pg *cs.PG, cl *http.Client, rapi *api.Api) *Builder {
	return &Builder{
		pg: pg,
		cache: lazymap.New[*StreamsResponse](&lazymap.Config{
			Expire:      1 * time.Minute,
			ErrorExpire: 10 * time.Second,
		}),
		domain: c.String(common.DomainFlag),
		rapi:   rapi,
		cl:     cl,
	}
}

func (s *Builder) BuildManifestService() (ManifestService, error) {
	return NewManifest(s.domain), nil
}

func (s *Builder) BuildCatalogService(uID uuid.UUID) (CatalogService, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database not initialized")
	}
	cas := NewLibrary(s.domain, db, uID, s.rapi, nil)

	return cas, nil
}

func (s *Builder) BuildMetaService(uID uuid.UUID) (MetaService, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database not initialized")
	}
	mes := NewLibrary(s.domain, db, uID, s.rapi, nil)

	return mes, nil
}

func (s *Builder) BuildStreamsService(uID uuid.UUID, cla *api.Claims) (StreamsService, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database not initialized")
	}
	acs, err := NewAddonCompositeStreamsByUserID(db, s.cl, uID, s.cache)
	if err != nil {
		return nil, err
	}
	sts := NewLibrary(s.domain, db, uID, s.rapi, cla)
	cs := NewCompositeStream([]StreamsService{sts, acs})
	ds := NewDedupStream(cs)
	es := NewEnrichStream(ds, s.rapi, cla)

	return es, nil
}

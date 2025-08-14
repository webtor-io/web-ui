package embed

import (
	"time"

	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/models"

	"github.com/go-pg/pg/v10"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/lazymap"
	"github.com/webtor-io/web-ui/services/claims"
)

type DomainSettings struct {
	lazymap.LazyMap[*DomainSettingsData]
	pg     *cs.PG
	claims *claims.Claims
}
type DomainSettingsData struct {
	Ads bool `json:"ads"`
}

func NewDomainSettings(pg *cs.PG, claims *claims.Claims) *DomainSettings {
	return &DomainSettings{
		pg:     pg,
		claims: claims,
		LazyMap: lazymap.New[*DomainSettingsData](&lazymap.Config{
			Expire:      time.Minute,
			ErrorExpire: 10 * time.Second,
		}),
	}
}

func (s *DomainSettings) Get(domain string) (*DomainSettingsData, error) {
	return s.LazyMap.Get(domain, func() (*DomainSettingsData, error) {
		if s.pg == nil || s.pg.Get() == nil || s.claims == nil {
			return &DomainSettingsData{}, nil
		}
		db := s.pg.Get()
		em := &models.EmbedDomain{}
		err := db.Model(em).
			Relation("User").
			Where("embed_domain.domain = ?", domain).
			Select()
		if errors.Is(err, pg.ErrNoRows) {
			return &DomainSettingsData{Ads: true}, nil
		} else if err != nil {
			return nil, err
		}
		cl, err := s.claims.Get(&claims.Request{
			Email:         em.User.Email,
			PatreonUserID: em.User.PatreonUserID,
		})
		if err != nil {
			return nil, err
		}
		return &DomainSettingsData{Ads: em.Ads || !cl.Claims.Embed.NoAds}, nil
	})
}

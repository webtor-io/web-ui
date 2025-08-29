package embed

import (
	"net/url"
	"time"

	"github.com/pkg/errors"
	"github.com/urfave/cli"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/common"

	"github.com/go-pg/pg/v10"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/lazymap"
	"github.com/webtor-io/web-ui/services/claims"
)

type DomainSettings struct {
	lazymap.LazyMap[*DomainSettingsData]
	pg     *cs.PG
	claims *claims.Claims
	domain string
}
type DomainSettingsData struct {
	Ads       bool         `json:"ads"`
	Rate      string       `json:"rate"`
	Found     bool         `json:"found"`
	Claims    *claims.Data `json:"-"`
	SessionID string       `json:"-"`
}

func NewDomainSettings(c *cli.Context, pg *cs.PG, claims *claims.Claims) (*DomainSettings, error) {
	d := c.String(common.DomainFlag)
	if d != "" {
		u, err := url.Parse(d)
		if err != nil {
			return nil, err
		}
		d = u.Hostname()
	}
	return &DomainSettings{
		pg:     pg,
		claims: claims,
		domain: d,
		LazyMap: lazymap.New[*DomainSettingsData](&lazymap.Config{
			Expire: time.Minute,
		}),
	}, nil
}

func (s *DomainSettings) Get(domain string) (*DomainSettingsData, error) {
	return s.LazyMap.Get(domain, func() (*DomainSettingsData, error) {
		if s.pg == nil || s.pg.Get() == nil || s.claims == nil {
			return &DomainSettingsData{}, nil
		}
		if domain == "localhost" || domain == "127.0.0.1" || domain == s.domain {
			return &DomainSettingsData{
				Ads:   true,
				Found: true,
			}, nil
		}
		db := s.pg.Get()
		em := &models.EmbedDomain{}
		err := db.Model(em).
			Relation("User").
			Where("embed_domain.domain = ?", domain).
			Select()
		if errors.Is(err, pg.ErrNoRows) {
			return &DomainSettingsData{
				Ads:   true,
				Found: false,
			}, nil
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
		ads := *em.Ads
		return &DomainSettingsData{
			Ads:    ads || !cl.Claims.Embed.NoAds,
			Found:  true,
			Claims: cl,
			SessionID: api.GenerateSessionIDFromUser(&auth.User{
				ID: em.User.UserID,
			}),
		}, nil
	})
}

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

const (
	useEmbed       = "use-embed"
	useAds         = "embed-use-ads"
	onlyAuthorized = "embed-only-authorized"
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.BoolTFlag{
			Name:   useEmbed,
			Usage:  "use embed",
			EnvVar: "USE_EMBED",
		},
		cli.BoolTFlag{
			Name:   useAds,
			Usage:  "embed use ads",
			EnvVar: "EMBED_USE_ADS",
		},
		cli.BoolFlag{
			Name:   onlyAuthorized,
			Usage:  "embed only authorized",
			EnvVar: "EMBED_ONLY_AUTHORIZED",
		},
	)
}

type DomainSettings struct {
	*lazymap.LazyMap[*DomainSettingsData]
	pg             *cs.PG
	claims         *claims.Claims
	domain         string
	useEmbed       bool
	onlyAuthorized bool
	useAds         bool
}
type DomainSettingsData struct {
	Ads          bool         `json:"ads"`
	Forbidden    bool         `json:"-"`
	Unauthorized bool         `json:"-"`
	Claims       *claims.Data `json:"-"`
	SessionID    string       `json:"-"`
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
		useEmbed:       c.BoolT(useEmbed),
		useAds:         c.BoolT(useAds),
		onlyAuthorized: c.Bool(onlyAuthorized),
	}, nil
}

func (s *DomainSettings) Get(domain string) (*DomainSettingsData, error) {
	if !s.useEmbed {
		return &DomainSettingsData{
			Forbidden: true,
		}, nil
	}
	return s.LazyMap.Get(domain, func() (*DomainSettingsData, error) {
		if s.pg == nil || s.pg.Get() == nil || s.claims == nil {
			return &DomainSettingsData{}, nil
		}
		if domain == "localhost" || domain == "127.0.0.1" || domain == s.domain {
			return &DomainSettingsData{
				Ads: s.useAds,
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
				Ads:          s.useAds,
				Unauthorized: s.onlyAuthorized,
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
			Ads:    s.useAds && (ads || !cl.Claims.Embed.NoAds),
			Claims: cl,
			SessionID: api.GenerateSessionIDFromUser(&auth.User{
				ID: em.User.UserID,
			}),
		}, nil
	})
}

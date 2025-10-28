package stremio

import (
	"context"
	"fmt"

	"github.com/webtor-io/web-ui/services/auth"
)

const catalogID = "Webtor.io"

type Manifest struct {
	domain string
	u      *auth.User
	ht     bool
}

func NewManifest(domain string, u *auth.User, hasToken bool) *Manifest {
	return &Manifest{
		domain: domain,
		u:      u,
		ht:     hasToken,
	}
}

func (s *Manifest) GetManifest(c context.Context) (*ManifestResponse, error) {
	m := &ManifestResponse{
		Id:          "org.stremio.webtor.io",
		Version:     "0.0.2",
		Name:        "Webtor.io",
		Description: "Stream your personal torrent library from Webtor directly in Stremio. Add torrents to your Webtor account and watch them instantly — no downloading, no setup, just click and play.",
		Types:       []string{"movie", "series"},
		Catalogs: []CatalogItem{
			{"movie", catalogID},
			{"series", catalogID},
		},
		Resources:    []string{"stream", "catalog", "meta"},
		Logo:         fmt.Sprintf("%v/assets/night/android-chrome-256x256.png", s.domain),
		ContactEmail: "support@webtor.io",
		AddonsConfig: &AddonsConfig{
			Issuer:    "https://stremio-addons.net",
			Signature: "eyJhbGciOiJkaXIiLCJlbmMiOiJBMTI4Q0JDLUhTMjU2In0..jgHUY1gMFbTnCL4khCAsCA.DUQP0jZs-KpFEpL6aC4FVV08q97uhZ1RnMm4vEfbpRI0OSd1NhQaN18MxsHf5Md6gUnnzjwwprX2IoX0iF4TtG-5mPRKx2z91964sa6NqsFX_QWx3sdn6HGllbTJG_-t.RVNoutseK8lRM7QapFttQg",
		},
	}
	if s.u == nil || !s.ht {
		m.BehaviorHints = &BehaviorHints{
			Configurable:          true,
			ConfigurationRequired: true,
		}

	}
	return m, nil
}

var _ ManifestService = (*Manifest)(nil)

package stremio

import (
	"context"
	"fmt"

	"github.com/webtor-io/web-ui/services/auth"
)

const catalogID = "Webtor.io"

// manifestVersion goes up whenever what the manifest says changes: Stremio
// keeps the manifest it installed, and a new version is what makes clients
// and addon catalogues pick up the new text.
const manifestVersion = "0.1.0"

// manifestDescription says what the addon is without promising speed: how
// fast a stream starts depends on the swarm. It quotes no free-plan cap
// either: the site's free cap does not apply here, because streams the
// addon plays through Webtor's servers need a paid plan (a user's own
// streaming backend is tried first — see services/link_resolver).
const manifestDescription = "Your Webtor library in Stremio, plus the Stremio addons you add to your Webtor profile, played through Webtor. " +
	"Torrents download on Webtor's servers, not on your device, so your IP address never joins the swarm. " +
	"Playing through Webtor's servers needs a Webtor plan."

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
		Version:     manifestVersion,
		Name:        "Webtor.io",
		Description: manifestDescription,
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

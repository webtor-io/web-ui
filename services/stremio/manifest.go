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
// fast a stream starts depends on the swarm, and how fast it plays on the
// plan, which the manifest can only quote from the catalog.
const manifestDescription = "Your Webtor library in Stremio, plus the Stremio addons you add to your Webtor profile, played through Webtor. " +
	"Torrents download on Webtor's servers, not on your device, so your IP address never joins the swarm."

type Manifest struct {
	domain string
	u      *auth.User
	ht     bool
	// freeMbps is the free plan's download cap from the offer catalog, 0
	// when there is none to quote (no catalog: nothing is sold, and a
	// deployment without plans has no cap to warn about).
	freeMbps int64
}

func NewManifest(domain string, u *auth.User, hasToken bool, freeMbps int64) *Manifest {
	return &Manifest{
		domain:   domain,
		u:        u,
		ht:       hasToken,
		freeMbps: freeMbps,
	}
}

// Description is the manifest's description: what the addon does, plus the
// free plan's cap when the catalog quotes one.
func (s *Manifest) Description() string {
	if s.freeMbps <= 0 {
		return manifestDescription
	}
	return fmt.Sprintf("%s Speed depends on your plan: the free one streams at up to %d\u00a0Mbit/s.", manifestDescription, s.freeMbps)
}

func (s *Manifest) GetManifest(c context.Context) (*ManifestResponse, error) {
	m := &ManifestResponse{
		Id:          "org.stremio.webtor.io",
		Version:     manifestVersion,
		Name:        "Webtor.io",
		Description: s.Description(),
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

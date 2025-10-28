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

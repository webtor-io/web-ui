package stremio

import (
	"context"
	"fmt"
)

const catalogID = "Webtor.io"

type Manifest struct {
	domain string
}

func NewManifest(domain string) *Manifest {
	return &Manifest{
		domain: domain,
	}
}

func (s *Manifest) GetManifest(_ context.Context) (*ManifestResponse, error) {
	return &ManifestResponse{
		Id:          "org.stremio.webtor.io",
		Version:     "0.0.1",
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
	}, nil
}

var _ ManifestService = (*Manifest)(nil)

package stremio

import (
	"context"

	"github.com/go-pg/pg/v10"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	ptn "github.com/webtor-io/web-ui/services/parse_torrent_name"
)

type PreferredStream struct {
	inner  StreamsService
	db     *pg.DB
	u      *auth.User
	cla    *claims.Data
	parser ptn.Parser
}

func NewPreferredStream(inner StreamsService, db *pg.DB, u *auth.User, cla *claims.Data) *PreferredStream {
	return &PreferredStream{
		inner: inner,
		db:    db,
		u:     u,
		cla:   cla,
		parser: ptn.NewCompoundParser([]ptn.Parser{
			ptn.GetFieldParser(ptn.FieldTypeResolution),
		}),
	}
}

func (s *PreferredStream) GetName() string {
	return "Preferred" + s.inner.GetName()
}

func (s *PreferredStream) GetStreams(ctx context.Context, contentType, contentID string) (*StreamsResponse, error) {
	us, err := GetUserSettingsDataByClaims(ctx, s.db, s.u.ID)
	if err != nil {
		return nil, err
	}
	resp, err := s.inner.GetStreams(ctx, contentType, contentID)
	if err != nil {
		return nil, err
	}
	streams, err := s.filterByPreferredResolutions(resp.Streams, us.PreferredResolutions)
	if err != nil {
		return nil, err
	}
	return &StreamsResponse{Streams: streams}, nil
}

// filterByPreferredResolutions keeps only streams whose name-parsed
// resolution is in the user's enabled set, emitting them in the user's
// preferred-resolution order.
//
// Library streams bypass the filter entirely for the same reason
// LangFilterStream exempts them (see isLibraryStream): the user already
// added these exact torrents to their Vault, so we must not hide them on a
// resolution preference. Dropping them here also broke Stremio
// binge-watching — resolution is parsed per-episode from the file name, so an
// episode whose name carries no resolution token (→ "other", which the user
// may have disabled) would vanish while its neighbours survived, leaving
// Stremio with no matching bingeGroup stream for the next episode and bouncing
// the viewer back to the source-selection screen. Exempt library streams are
// emitted first, ahead of the resolution-ordered addon streams.
func (s *PreferredStream) filterByPreferredResolutions(streams []StreamItem, prefs []models.ResolutionSetting) ([]StreamItem, error) {
	groups := make(map[string][]StreamItem)
	for _, resolution := range prefs {
		if resolution.Enabled {
			groups[resolution.Resolution] = []StreamItem{}
		}
	}
	var libraryStreams []StreamItem
	for _, st := range streams {
		if isLibraryStream(&st) {
			libraryStreams = append(libraryStreams, st)
			continue
		}
		ti := &ptn.TorrentInfo{}
		ms := ptn.Matches{}
		ms, err := s.parser.Parse(st.Name, ms)
		if err != nil {
			return nil, err
		}
		ti.Map(ms)
		var res string
		if ti.Resolution != "" {
			res = ti.Resolution
		} else {
			res = "other"
		}
		if res == "2160p" {
			res = "4k"
		}
		if _, ok := groups[res]; ok {
			groups[res] = append(groups[res], st)
		}
	}
	out := append([]StreamItem{}, libraryStreams...)
	for _, resolution := range prefs {
		if _, ok := groups[resolution.Resolution]; ok {
			out = append(out, groups[resolution.Resolution]...)
		}
	}
	return out, nil
}

var _ StreamsService = (*PreferredStream)(nil)

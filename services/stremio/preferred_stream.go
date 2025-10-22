package stremio

import (
	"context"

	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/services/claims"
	ptn "github.com/webtor-io/web-ui/services/parse_torrent_name"
)

type PreferredStream struct {
	inner  StreamsService
	db     *pg.DB
	userID uuid.UUID
	cla    *claims.Data
	parser ptn.Parser
}

func NewPreferredStream(inner StreamsService, db *pg.DB, userID uuid.UUID, cla *claims.Data) *PreferredStream {
	return &PreferredStream{
		inner:  inner,
		db:     db,
		userID: userID,
		cla:    cla,
		parser: ptn.NewCompoundParser([]ptn.Parser{
			ptn.GetFieldParser(ptn.FieldTypeResolution),
		}),
	}
}

func (s *PreferredStream) GetName() string {
	return "Preferred" + s.inner.GetName()
}

func (s *PreferredStream) GetStreams(ctx context.Context, contentType, contentID string) (*StreamsResponse, error) {
	us, err := GetUserSettingsDataByClaims(ctx, s.db, s.userID)
	if err != nil {
		return nil, err
	}
	groups := make(map[string][]StreamItem)
	for _, resolution := range us.PreferredResolutions {
		if resolution.Enabled {
			groups[resolution.Resolution] = []StreamItem{}
		}
	}
	resp, err := s.inner.GetStreams(ctx, contentType, contentID)
	if err != nil {
		return nil, err
	}
	for _, st := range resp.Streams {
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
	var streams []StreamItem
	for _, resolution := range us.PreferredResolutions {
		if _, ok := groups[resolution.Resolution]; ok {
			streams = append(streams, groups[resolution.Resolution]...)
		}
	}
	fresp := &StreamsResponse{Streams: streams}

	return fresp, nil
}

var _ StreamsService = (*PreferredStream)(nil)

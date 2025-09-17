package stremio

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
)

const idPrefix = "wt-"

type Library struct {
	db     *pg.DB
	uID    uuid.UUID
	domain string
	sapi   *api.Api
	cla    *api.Claims
}

func NewLibrary(domain string, db *pg.DB, uID uuid.UUID, sapi *api.Api, cla *api.Claims) *Library {
	return &Library{
		db:     db,
		uID:    uID,
		domain: domain,
		sapi:   sapi,
		cla:    cla,
	}
}

func (s *Library) GetCatalog(ctx context.Context, ct string) (*MetasResponse, error) {
	vcs, err := s.getCatalogData(ctx, ct)
	if err != nil {
		return nil, err
	}
	var metas []MetaItem
	for _, vc := range vcs {
		metas = append(metas, s.makeMeta(vc))
	}

	return &MetasResponse{Metas: metas}, nil
}

var _ CatalogService = (*Library)(nil)

func (s *Library) GetMeta(ctx context.Context, ct, contentID string) (*MetaResponse, error) {
	args, err := s.bindArgs(ct, contentID)
	if err != nil {
		return nil, err
	}
	vcs, err := s.getMetaDataByID(ctx, ct, args.ID)
	if err != nil {
		return nil, err
	}
	if vcs == nil && len(vcs) == 0 {
		return nil, nil
	}
	vc := vcs[0]
	meta := s.makeMeta(vc)
	if ct == "series" {
		meta.Videos, err = s.makeVideos(vc)
	}
	return &MetaResponse{Meta: meta}, nil
}

var _ MetaService = (*Library)(nil)

func (s *Library) GetStreams(ctx context.Context, ct, contentID string) (*StreamsResponse, error) {
	args, err := s.bindArgs(ct, contentID)
	if err != nil {
		return nil, err
	}
	vcs, err := s.getMetaDataByID(ctx, ct, args.ID)
	if err != nil {
		return nil, err
	}

	var streams []StreamItem
	for _, vc := range vcs {
		si, err := s.getStreamItem(ctx, vc, ct, args)

		if err != nil {
			return nil, err
		}

		if si != nil {
			streams = append(streams, *si)
		}

	}
	return &StreamsResponse{
		Streams: streams,
	}, nil
}

func (s *Library) GetName() string {
	return "Library"
}

var _ StreamsService = (*Library)(nil)

func (s *Library) getCatalogData(ctx context.Context, t string) ([]models.VideoContentWithMetadata, error) {
	var items []models.VideoContentWithMetadata
	if t == "movie" {
		ls, err := models.GetLibraryMovieList(ctx, s.db, s.uID, models.SortTypeRecentlyAdded)
		if err != nil {
			return nil, err
		}
		items = make([]models.VideoContentWithMetadata, len(ls))
		for i, v := range ls {
			items[i] = v
		}
	} else if t == "series" {
		ls, err := models.GetLibrarySeriesList(ctx, s.db, s.uID, models.SortTypeRecentlyAdded)
		if err != nil {
			return nil, err
		}
		items = make([]models.VideoContentWithMetadata, len(ls))
		for i, v := range ls {
			items[i] = v
		}
	}
	return items, nil
}

func (s *Library) makeStreamURL(ctx context.Context, cla *api.Claims, resourceID string, id string) (string, error) {
	er, err := s.sapi.ExportResourceContent(ctx, cla, resourceID, id, "")
	if err != nil {
		return "", err
	}
	return er.ExportItems["download"].URL, nil
}

func (s *Library) makeStreamTitle(title string, md map[string]any) string {
	if quality, ok := md["quality"]; ok && strings.TrimSpace(quality.(string)) != "" {
		title = title + "." + quality.(string)
	}
	if resolution, ok := md["resolution"]; ok && strings.TrimSpace(resolution.(string)) != "" {
		title = title + "." + resolution.(string)
	}
	if container, ok := md["container"]; ok && strings.TrimSpace(container.(string)) != "" {
		title = title + "." + container.(string)
	}
	return title
}

type Args struct {
	ID      string
	Season  int
	Episode int
}

func (s *Library) bindArgs(ct, id string) (args *Args, err error) {
	if ct == "movie" {
		args = &Args{
			ID: id,
		}
		return
	}
	parts := strings.Split(id, ":")
	id = parts[0]
	var season, episode int
	if len(parts) > 1 {
		season, err = strconv.Atoi(parts[1])
		if err != nil {
			return
		}
		episode, err = strconv.Atoi(parts[2])
		if err != nil {
			return
		}
	}
	args = &Args{
		ID:      id,
		Season:  season,
		Episode: episode,
	}
	return
}

func (s *Library) getStreamItem(ctx context.Context, vc models.VideoContentWithMetadata, ct string, args *Args) (*StreamItem, error) {
	var su, title, p string
	var err error
	var idx int
	if ct == "movie" {
		p = *vc.GetPath()
		title = s.makeStreamTitle(vc.GetContent().Title, vc.GetContent().Metadata)

	} else if ct == "series" {
		ep := vc.GetEpisode(args.Season, args.Episode)
		if ep == nil {
			return nil, nil
		}
		p = *ep.Path
		title = s.makeStreamTitle(fmt.Sprintf("%v.S%vE%v", vc.GetContent().Title, args.Season, args.Episode), ep.Metadata)
	}
	ti, idx, err := s.retrieveTorrentItem(ctx, vc.GetContent().ResourceID, s.cla, p)
	if err != nil {
		return nil, err
	}
	su, err = s.makeStreamURL(ctx, s.cla, vc.GetContent().ResourceID, ti.ID)
	if err != nil {
		return nil, err
	}
	return &StreamItem{
		Title:    title,
		Url:      su,
		InfoHash: vc.GetContent().ResourceID,
		FileIdx:  uint8(idx),
		BehaviorHints: &StreamBehaviorHints{
			Filename: ti.Name,
		},
	}, nil

}

func (s *Library) getMetaDataByID(ctx context.Context, ct, id string) ([]models.VideoContentWithMetadata, error) {
	isVideoID := true
	if strings.HasPrefix(id, idPrefix) {
		id = strings.TrimPrefix(id, idPrefix)
		isVideoID = false
	}
	if s.db == nil {
		return nil, errors.New("database not initialized")
	}
	if !isVideoID {
		var vc models.VideoContentWithMetadata
		var err error
		if ct == "movie" {
			vc, err = models.GetMovieByID(ctx, s.db, s.uID, id)
		} else if ct == "series" {
			vc, err = models.GetSeriesByID(ctx, s.db, s.uID, id)
		}
		if err != nil {
			return nil, err
		}
		return []models.VideoContentWithMetadata{vc}, nil
	} else {
		var vcs []models.VideoContentWithMetadata
		if ct == "movie" {
			ls, err := models.GetMoviesByVideoID(ctx, s.db, s.uID, id)
			if err != nil {
				return nil, err
			}
			for _, l := range ls {
				vcs = append(vcs, l)
			}
		} else if ct == "series" {
			ls, err := models.GetSeriesByVideoID(ctx, s.db, s.uID, id)
			if err != nil {
				return nil, err
			}
			for _, l := range ls {
				vcs = append(vcs, l)
			}
		}
		return vcs, nil
	}
}

func (s *Library) makePoster(vc models.VideoContentWithMetadata) string {
	return fmt.Sprintf("%v/lib/%v/poster/%v/240.jpg", s.domain, vc.GetContentType(), vc.GetMetadata().VideoID)
}

func (s *Library) makeMeta(vc models.VideoContentWithMetadata) MetaItem {
	if vc.GetMetadata() != nil {
		return s.makeMetaWithMetadata(vc)
	} else {
		return s.makeMetaWithoutMetadata(vc)
	}

}
func (s *Library) makeMetaWithMetadata(vc models.VideoContentWithMetadata) MetaItem {
	meta := MetaItem{
		ID:          vc.GetMetadata().VideoID,
		Type:        string(vc.GetContentType()),
		Name:        vc.GetMetadata().Title,
		PosterShape: "poster",
	}
	if vc.GetMetadata().Year != nil {
		y := *vc.GetMetadata().Year
		meta.ReleaseInfo = strconv.Itoa(int(y))
	}
	if vc.GetMetadata().PosterURL != "" {
		meta.Poster = s.makePoster(vc)
	}
	return meta
}

func (s *Library) makeMetaWithoutMetadata(vc models.VideoContentWithMetadata) MetaItem {
	meta := MetaItem{
		ID:          idPrefix + vc.GetID().String(),
		Type:        string(vc.GetContentType()),
		Name:        vc.GetContent().Title,
		PosterShape: "poster",
	}
	if vc.GetContent().Year != nil {
		y := *vc.GetContent().Year
		meta.ReleaseInfo = strconv.Itoa(int(y))
	}
	return meta
}

func (s *Library) retrieveTorrentItem(ctx context.Context, hash string, claims *api.Claims, path string) (*ra.ListItem, int, error) {
	limit := uint(100)
	offset := uint(0)
	var idx int
	for {
		resp, err := s.sapi.ListResourceContentCached(ctx, claims, hash, &api.ListResourceContentArgs{
			Limit:  limit,
			Offset: offset,
		})
		if err != nil {
			return nil, 0, err
		}
		for _, item := range resp.Items {
			if item.PathStr == path {
				return &item, idx, nil
			}
			if item.Type == ra.ListTypeFile {
				idx++
			}
		}
		if (resp.Count - int(offset)) == len(resp.Items) {
			break
		}
		offset += limit
	}
	return nil, 0, nil
}

func (s *Library) makeVideos(vc models.VideoContentWithMetadata) ([]VideoItem, error) {
	se, _ := vc.(*models.Series)
	var vis []VideoItem
	for _, e := range se.Episodes {
		var ep, sea int
		if e.Episode != nil {
			ee := *e.Episode
			ep = int(ee)
		}
		if e.Season != nil {
			ss := *e.Season
			sea = int(ss)
		}
		var id string
		if vc.GetMetadata() != nil {
			id = vc.GetMetadata().VideoID
		} else {
			id = fmt.Sprintf("%v%v", idPrefix, vc.GetID().String())
		}
		vi := VideoItem{
			Name:    fmt.Sprintf("Episode %d", ep),
			ID:      fmt.Sprintf("%v:%v:%v", id, sea, ep),
			Season:  sea,
			Episode: ep,
		}
		vis = append(vis, vi)
	}
	return vis, nil
}

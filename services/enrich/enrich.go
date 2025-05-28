package enrich

import (
	"context"
	"encoding/json"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	services "github.com/webtor-io/common-services"
	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	ptn "github.com/webtor-io/web-ui/services/parse_torrent_name"
	"strings"
	"time"
)

type Enricher struct {
	pg      *services.PG
	api     *api.Api
	mappers []MetadataMapper
}

type MetadataMapper interface {
	Map(ctx context.Context, query *models.VideoContent, getType models.ContentType, force bool) (*models.VideoMetadata, error)
	GetName() string
}

func NewEnricher(pg *services.PG, api *api.Api, mappers []MetadataMapper) *Enricher {
	return &Enricher{
		pg:      pg,
		api:     api,
		mappers: mappers,
	}
}

func StructToMap(s interface{}) (map[string]interface{}, error) {
	var result map[string]interface{}

	data, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

type TorrentInfo struct {
	*ptn.TorrentInfo
	*ra.ListItem
}

func MakeTorrentInfo(item *ra.ListItem) (*TorrentInfo, error) {
	ti, err := parseItem(item)
	if err != nil {
		return nil, err
	}

	return &TorrentInfo{
		TorrentInfo: ti,
		ListItem:    item,
	}, nil
}

func parseItem(item *ra.ListItem) (ti *ptn.TorrentInfo, err error) {
	ti = &ptn.TorrentInfo{}
	pathParts := strings.Split(item.PathStr, "/")
	for _, part := range pathParts {
		if part == "" {
			continue
		}
		ti, err = ptn.Parse(ti, part)
		if err != nil {
			return nil, err
		}
	}
	return ti, nil
}

func (s *Enricher) Enrich(ctx context.Context, hash string, claims *api.Claims, force bool) error {
	log.Infof("enriching media info for hash %s", hash)
	db := s.pg.Get()
	if db == nil {
		return errors.New("db is nil")
	}
	mi, err := models.TryInsertOrLockMediaInfo(ctx, db, hash, 24*time.Hour, force)
	if err != nil {
		return err
	}
	if mi == nil {
		log.Infof("no media info acquired for hash %s", hash)
		return nil
	}
	log.Infof("start processing media info %+v", mi)

	items, err := s.retrieveTorrentItems(ctx, hash, claims)

	if err != nil {
		return err
	}

	//series := map[string]*models.Series{}
	//var movies []*models.Movie
	var torrentInfos []*TorrentInfo

	for _, item := range items {
		if item.MediaFormat != ra.Video {
			continue
		}
		ti, err := MakeTorrentInfo(&item)
		if err != nil {
			return err
		}
		torrentInfos = append(torrentInfos, ti)
	}

	if len(torrentInfos) == 0 {
		log.Infof("no media info acquired for hash %s", hash)
		mi.Status = int16(models.MediaInfoStatusNoMedia)
		err = models.UpdateMediaInfo(ctx, db, mi)
		if err != nil {
			return err
		}
		return nil
	}

	log.Infof("got %v media items", len(torrentInfos))

	mt := s.getMediaType(torrentInfos)

	log.Infof("got media type %v for hash %v", mt, hash)

	var series *models.Series
	var movie *models.Movie

	if mt == models.MediaInfoMediaTypeMovieSingle {
		movie, err = s.makeMovie(torrentInfos, hash)
		if err != nil {
			return errors.Wrapf(err, "failed to make movie for hash %s", hash)
		}
	} else {
		series, err = s.makeSeriesWithEpisodes(torrentInfos, hash, mt)
		if err != nil {
			return errors.Wrapf(err, "failed to make series for hash %s", hash)
		}
	}
	var movies []*models.Movie
	if movie != nil {
		movies = append(movies, movie)
	}

	err = models.ReplaceMoviesForResource(ctx, db, hash, movies)
	if err != nil {
		return errors.Wrapf(err, "failed to replace movie for hash %s", hash)
	}

	var seriesSlice []*models.Series
	if series != nil {
		seriesSlice = append(seriesSlice, series)
	}
	err = models.ReplaceSeriesForResource(ctx, db, hash, seriesSlice)
	if err != nil {
		return errors.Wrapf(err, "failed to replace series for hash %s", hash)
	}

	movies, err = models.GetMoviesByResourceID(ctx, db, hash)

	if err != nil {
		return errors.Wrapf(err, "failed to get movies for hash %s", hash)
	}

	for _, m := range movies {
		md, err := s.mapMetadata(ctx, m.VideoContent, m.GetContentType(), force)
		if err != nil {
			return errors.Wrapf(err, "failed to map metadata for movie %+v hash %s", m, hash)
		}
		if md == nil {
			log.Warnf("no metadata for %v", m.VideoContent)
			continue
		}
		log.Infof("processing movie metadata %+v", md)
		metadataID, err := models.UpsertMovieMetadata(ctx, db, md)
		if err != nil {
			return errors.Wrapf(err, "failed to upsert metadata for movie %+v hash %s", md, hash)
		}
		err = models.LinkMovieToMetadata(ctx, db, m.MovieID, metadataID)
		if err != nil {
			return errors.Wrapf(err, "failed to link movie %+v with metadata for hash %s", m, hash)
		}
	}

	seriesSlice, err = models.GetSeriesByResourceID(ctx, db, hash)
	if err != nil {
		return errors.Wrapf(err, "failed to get series for hash %s", hash)
	}

	for _, ser := range seriesSlice {
		var md *models.VideoMetadata
		if mt != models.MediaInfoMediaTypeSeriesCompilation && mt != models.MediaInfoMediaTypeSeriesSplitScenes {
			md, err = s.mapMetadata(ctx, ser.VideoContent, ser.GetContentType(), force)
		}
		if err != nil {
			return errors.Wrapf(err, "failed to map metadata for hash %s", hash)
		}
		if md == nil {
			log.Warnf("no metadata for %v", ser.VideoContent)
			continue
		}
		log.Infof("processing series %+v", md)
		metadataID, err := models.UpsertSeriesMetadata(ctx, db, md)
		if err != nil {
			return errors.Wrapf(err, "failed to upsert series metadata for hash %s", hash)
		}
		err = models.LinkSeriesToMetadata(ctx, db, ser.SeriesID, metadataID)
		if err != nil {
			return errors.Wrapf(err, "failed to link series %+v with metadata for hash %s", ser, hash)
		}
	}

	mi.Status = int16(models.MediaInfoStatusDone)
	mtInt16 := int16(mt)
	mi.MediaType = &mtInt16
	err = models.UpdateMediaInfo(ctx, db, mi)
	if err != nil {
		return err
	}

	return nil
}
func (s *Enricher) mapMetadata(ctx context.Context, vc *models.VideoContent, t models.ContentType, f bool) (md *models.VideoMetadata, err error) {
	for _, m := range s.mappers {
		md, err = m.Map(ctx, vc, t, f)
		if err != nil {
			return nil, errors.Wrapf(err, "got \"%v\" mapper error", m.GetName())
		}
		if md != nil {
			return
		}
	}
	return
}

func (s *Enricher) retrieveTorrentItems(ctx context.Context, hash string, claims *api.Claims) ([]ra.ListItem, error) {
	limit := uint(100)
	offset := uint(0)
	var items []ra.ListItem
	for {
		resp, err := s.api.ListResourceContent(ctx, claims, hash, &api.ListResourceContentArgs{
			Limit:  limit,
			Offset: offset,
		})
		if err != nil {
			return nil, err
		}
		for _, item := range resp.Items {
			items = append(items, item)
		}
		if (resp.Count - int(offset)) == len(resp.Items) {
			break
		}
		offset += limit
	}
	return items, nil
}

func (s *Enricher) getMediaType(infos []*TorrentInfo) models.MediaInfoMediaType {
	var hasSeasones, hasDifferentSeasones, hasEpisodes, sameTitle, hasScenes bool
	sameTitle = true
	var title string
	var season int
	for _, info := range infos {
		if info.Season != 0 && season != info.Season && season != 0 {
			hasDifferentSeasones = true
		}
		if info.Season != 0 {
			season = info.Season
			hasSeasones = true
		}
		if info.Episode != 0 {
			hasEpisodes = true
		}
		if info.Scene != 0 {
			hasScenes = true
		}
		if title != "" && info.Title != title {
			sameTitle = false
		}
		title = info.Title
	}
	if len(infos) < 4 && sameTitle && !hasEpisodes && !hasScenes && !hasSeasones {
		return models.MediaInfoMediaTypeMovieSingle
	} else if hasSeasones && hasEpisodes && hasDifferentSeasones {
		return models.MediaInfoMediaTypeSeriesMultipleSeasons
	} else if hasEpisodes && !hasDifferentSeasones {
		return models.MediaInfoMediaTypeSeriesSingleSeason
	} else if hasScenes && !hasDifferentSeasones {
		return models.MediaInfoMediaTypeSeriesSplitScenes
	} else {
		return models.MediaInfoMediaTypeSeriesCompilation
	}
}

func (s *Enricher) makeMovie(infos []*TorrentInfo, hash string) (*models.Movie, error) {
	ti := infos[0]
	movie := &models.Movie{
		VideoContent: &models.VideoContent{},
		ResourceID:   hash,
	}
	movie.Title = ti.Title
	if ti.Year != 0 {
		year := int16(ti.Year)
		movie.Year = &year
	}
	movie.Path = &ti.PathStr
	metadata, err := StructToMap(ti.TorrentInfo)
	if err != nil {
		return nil, err
	}
	movie.Metadata = metadata
	return movie, nil
}

func (s *Enricher) makeSeriesWithEpisodes(infos []*TorrentInfo, hash string, mt models.MediaInfoMediaType) (*models.Series, error) {
	var ti *TorrentInfo
	var err error
	if mt == models.MediaInfoMediaTypeSeriesCompilation || mt == models.MediaInfoMediaTypeSeriesSplitScenes {
		ti, err = s.makeCompilationTorrentInfo(infos)
	} else {
		ti, err = s.makeStandardSeriesTorrentInfo(infos)
	}
	if err != nil {
		return nil, err
	}
	ser := &models.Series{
		VideoContent: &models.VideoContent{},
		SeriesID:     uuid.NewV4(),
		ResourceID:   hash,
	}
	ser.Title = ti.Title
	if ti.Year != 0 {
		year := int16(ti.Year)
		ser.Year = &year
	}
	for _, ti = range infos {
		episode := ti.Episode
		if episode == 0 {
			episode = ti.Scene
		}
		var sea *int16
		if ti.Season != 0 {
			seaInt16 := int16(ti.Season)
			sea = &seaInt16
		}
		var ep *int16
		if episode != 0 {
			epInt16 := int16(episode)
			ep = &epInt16
		}
		e := &models.Episode{
			ResourceID: hash,
			SeriesID:   ser.SeriesID,
			Path:       &ti.PathStr,
			Season:     sea,
			Episode:    ep,
		}
		metadata, err := StructToMap(ti.TorrentInfo)
		if err != nil {
			return nil, err
		}
		e.Metadata = metadata
		ser.Episodes = append(ser.Episodes, e)
	}
	return ser, nil
}

func (s *Enricher) makeCompilationTorrentInfo(infos []*TorrentInfo) (*TorrentInfo, error) {
	ti := &ptn.TorrentInfo{}
	pathParts := strings.Split(strings.TrimPrefix(infos[0].PathStr, "/"), "/")
	ti, err := ptn.Parse(ti, pathParts[0])
	if err != nil {
		return nil, err
	}
	return &TorrentInfo{
		ListItem:    infos[0].ListItem,
		TorrentInfo: ti,
	}, nil
}

func (s *Enricher) makeStandardSeriesTorrentInfo(infos []*TorrentInfo) (*TorrentInfo, error) {
	for _, info := range infos {
		if info.Episode != 0 {
			return info, nil
		}
	}
	return nil, errors.New("no episode info in torrent list")
}

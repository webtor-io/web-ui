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

	log.Infof("got %v items", len(items))

	if err != nil {
		return err
	}

	series := map[string]*models.Series{}
	var movies []*models.Movie

	mediaCount := 0

	for _, item := range items {
		if item.MediaFormat != ra.Video {
			continue
		}
		mediaCount++
		log.Infof("processing item %+v", item)
		ti, err2 := s.parseItem(item)
		if err2 != nil {
			return err2
		}
		log.Infof("got torrent info %+v", ti)
		if ti.Episode == 0 && ti.Scene == 0 && ti.Season == 0 && ti.Title != "" {
			movie := &models.Movie{
				VideoContent: &models.VideoContent{},
				ResourceID:   hash,
			}
			movie.Title = ti.Title
			if ti.Year != 0 {
				year := int16(ti.Year)
				movie.Year = &year
			}
			movie.Path = &item.PathStr
			metadata, err := StructToMap(ti)
			if err != nil {
				return err
			}
			movie.Metadata = metadata
			movies = append(movies, movie)
		} else if (ti.Episode != 0 || ti.Scene != 0) && ti.Title != "" {
			key := strings.ToLower(ti.Title)
			s, ok := series[key]
			if !ok {
				s = &models.Series{
					VideoContent: &models.VideoContent{},
					SeriesID:     uuid.NewV4(),
					ResourceID:   hash,
				}
				s.Title = ti.Title
				if ti.Year != 0 {
					year := int16(ti.Year)
					s.Year = &year
				}
				series[key] = s
			}
			episode := ti.Episode
			if episode == 0 {
				episode = ti.Scene
			}
			e := &models.Episode{
				ResourceID: hash,
				SeriesID:   s.SeriesID,
				Path:       &item.PathStr,
				Season:     int16(ti.Season),
				Episode:    int16(episode),
			}
			metadata, err := StructToMap(ti)
			if err != nil {
				return err
			}
			e.Metadata = metadata
			s.Episodes = append(s.Episodes, e)
		}
	}
	err = models.ReplaceMoviesForResource(ctx, db, hash, movies)
	if err != nil {
		return err
	}

	var seriesSlice []*models.Series

	for _, s := range series {
		seriesSlice = append(seriesSlice, s)
	}

	err = models.ReplaceSeriesForResource(ctx, db, hash, seriesSlice)
	if err != nil {
		return err
	}
	mi.HasMovie = len(movies) > 0
	mi.HasSeries = len(seriesSlice) > 0
	mi.MediaCount = int16(mediaCount)
	if mediaCount == 0 {
		mi.Status = int16(models.MediaInfoStatusNoMedia)
		return nil
	}

	movies, err = models.GetMoviesByResourceID(ctx, db, hash)
	if err != nil {
		return err
	}

	for _, m := range movies {
		md, err := s.mapMetadata(ctx, m.VideoContent, m.GetContentType(), force)
		if err != nil {
			return err
		}
		if md == nil {
			log.Warnf("no metadata for %v", m.VideoContent)
			continue
		}
		log.Infof("processing movie %+v", md)
		metadataID, err := models.UpsertMovieMetadata(ctx, db, md)
		if err != nil {
			return err
		}
		err = models.LinkMovieToMetadata(ctx, db, m.MovieID, metadataID)
		if err != nil {
			return err
		}
	}

	seriesSlice, err = models.GetSeriesByResourceID(ctx, db, hash)
	if err != nil {
		return err
	}

	for _, ser := range seriesSlice {
		md, err := s.mapMetadata(ctx, ser.VideoContent, ser.GetContentType(), force)
		if err != nil {
			return err
		}
		if md == nil {
			log.Warnf("no metadata for %v", ser.VideoContent)
			continue
		}
		log.Infof("processing series %+v", md)
		metadataID, err := models.UpsertSeriesMetadata(ctx, db, md)
		if err != nil {
			return err
		}
		err = models.LinkSeriesToMetadata(ctx, db, ser.SeriesID, metadataID)
		if err != nil {
			return err
		}
	}

	mi.Status = int16(models.MediaInfoStatusDone)
	err = models.UpdateMediaInfo(ctx, db, mi)
	if err != nil {
		return err
	}

	return nil
}
func (s *Enricher) mapMetadata(ctx context.Context, vc *models.VideoContent, t models.ContentType, f bool) (md *models.VideoMetadata, err error) {
	for _, m := range s.mappers {
		md, err = m.Map(ctx, vc, t, f)
		if err == nil && md != nil {
			return
		}
	}
	return
}

func (s *Enricher) parseItem(item ra.ListItem) (ti *ptn.TorrentInfo, err error) {
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

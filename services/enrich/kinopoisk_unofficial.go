package enrich

import (
	"context"
	"fmt"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	km "github.com/webtor-io/web-ui/models/kinopoisk_unofficial"
	ku "github.com/webtor-io/web-ui/services/kinopoisk_unofficial"
)

type KinopoiskUnofficial struct {
	pg  *cs.PG
	api *ku.Api
}

func (s *KinopoiskUnofficial) GetName() string {
	return "Kinopoisk Unofficial"
}

func NewKinopoiskUnofficial(pg *cs.PG, api *ku.Api) *KinopoiskUnofficial {
	return &KinopoiskUnofficial{
		pg:  pg,
		api: api,
	}
}

func (s *KinopoiskUnofficial) makeVideoMetadata(mi *km.Info) *models.VideoMetadata {
	description, _ := mi.Metadata["description"].(string)
	posterURL, _ := mi.Metadata["posterUrl"].(string)
	var rating *float64
	ratingFloat, ok := mi.Metadata["ratingImdb"].(float64)
	if !ok {
		ratingFloat, _ = mi.Metadata["ratingKinopoisk"].(float64)
	}
	if ratingFloat != 0 {
		rating = &ratingFloat
	}
	imdbID := mi.ImdbID
	if imdbID == nil {
		fakeID := fmt.Sprintf("kp%v", mi.KpID)
		imdbID = &fakeID
	}
	return &models.VideoMetadata{
		VideoID:   *imdbID,
		Title:     mi.Title,
		Year:      mi.Year,
		Plot:      description,
		PosterURL: posterURL,
		Rating:    rating,
	}
}

func (s *KinopoiskUnofficial) Map(ctx context.Context, m *models.VideoContent, ct models.ContentType, force bool) (*models.VideoMetadata, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("db is nil")
	}
	q, err := km.GetQuery(ctx, db, m.Title, m.Year)
	if err != nil {
		return nil, err
	}
	if q != nil && !force {
		if q.KpID == nil {
			return nil, nil
		}
		mi, err := km.GetInfoByID(ctx, db, *q.KpID)
		if err != nil {
			return nil, err
		}
		if mi == nil {
			return nil, nil
		}

		return s.makeVideoMetadata(mi), nil
	}
	res, err := s.api.SearchByTitleAndYear(ctx, m.Title, m.Year)
	if err != nil {
		return nil, err
	}
	if len(res.Films) == 0 {
		log.Infof("no kinopoisk movie found for title %v and year %v", m.Title, m.Year)
		_, err = km.InsertQueryIgnoreConflict(ctx, db, m.Title, m.Year, nil)
		if err != nil {
			return nil, err
		}
		return nil, nil
	}
	data, err := s.api.GetByKpID(ctx, res.Films[0].FilmID)
	if err != nil {
		return nil, err
	}
	_, err = km.InsertQueryIgnoreConflict(ctx, db, m.Title, m.Year, &data.KinopoiskID)
	mi, err := km.UpsertInfo(ctx, db, data.KinopoiskID, data.Raw)
	if err != nil {
		return nil, err
	}
	return s.makeVideoMetadata(mi), nil
}

var _ MetadataMapper = (*KinopoiskUnofficial)(nil)

package enrich

import (
	"context"
	"fmt"
	"strconv"
	"strings"

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
	// Always identify the record by our internal kp{id}, never by the
	// imdbId Kinopoisk Unofficial advertises in their response. Their
	// imdbId field is unreliable: it can point to a different film with
	// the same English title (e.g. kp=306084 "Теория большого взрыва"
	// → tt1147717, which is actually a 2007 short, not the CBS sitcom
	// tt0898266). Routing posters via that id then breaks because TMDB
	// has no record for it. Keeping id and poster from the same source
	// is an invariant — see migration 51 for the historical cleanup.
	return &models.VideoMetadata{
		VideoID:   fmt.Sprintf("kp%v", mi.KpID),
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

func (s *KinopoiskUnofficial) MapByID(ctx context.Context, videoID string, ct models.ContentType, force bool) (*models.VideoMetadata, error) {
	if !strings.HasPrefix(videoID, "kp") {
		return nil, nil
	}
	kpID, err := strconv.Atoi(strings.TrimPrefix(videoID, "kp"))
	if err != nil {
		return nil, nil
	}

	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("db is nil")
	}

	if !force {
		mi, err := km.GetInfoByID(ctx, db, kpID)
		if err != nil {
			return nil, err
		}
		if mi != nil {
			return s.makeVideoMetadata(mi), nil
		}
	}

	data, err := s.api.GetByKpID(ctx, kpID)
	if err != nil {
		return nil, err
	}

	mi, err := km.UpsertInfo(ctx, db, data.KinopoiskID, data.Raw)
	if err != nil {
		return nil, err
	}

	return s.makeVideoMetadata(mi), nil
}

var _ MetadataMapper = (*KinopoiskUnofficial)(nil)
var _ DirectMapper = (*KinopoiskUnofficial)(nil)

package enrich

import (
	"context"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	om "github.com/webtor-io/web-ui/models/omdb"
	"github.com/webtor-io/web-ui/services/omdb"
	"strconv"
)

type OMDB struct {
	api *omdb.Api
	pg  *cs.PG
}

func NewOMDB(pg *cs.PG, api *omdb.Api) *OMDB {
	if api == nil {
		return nil
	}
	return &OMDB{
		pg:  pg,
		api: api,
	}
}

const NA = "N/A"

type OmdbMetadata struct {
	*om.Info
}

func NewOmdbMetadata(info *om.Info) *OmdbMetadata {
	return &OmdbMetadata{
		Info: info,
	}
}

func (s *OmdbMetadata) MakeVideoMetadata() *models.VideoMetadata {
	return &models.VideoMetadata{
		VideoID:   s.GetImdbID(),
		Title:     s.GetTitle(),
		Year:      s.GetYear(),
		Plot:      s.GetPlot(),
		PosterURL: s.GetPosterURL(),
		Rating:    s.GetImdbRating(),
	}
}

func (s *OmdbMetadata) GetYear() *int16 {
	return s.Info.Year
}

func (s *OmdbMetadata) GetTitle() string {
	return s.Title
}

func (s *OmdbMetadata) GetPlot() string {
	poster, _ := s.Metadata["Plot"].(string)
	if poster == NA {
		return ""
	}
	return poster
}

func (s *OmdbMetadata) GetImdbID() string {
	return s.ImdbID
}

func (s *OmdbMetadata) GetImdbRating() *float64 {
	rating, _ := s.Metadata["imdbRating"].(string)
	if rating == NA {
		return nil
	}
	pf, _ := strconv.ParseFloat(rating, 64)
	return &pf
}

func (s *OmdbMetadata) GetPosterURL() string {
	poster, _ := s.Metadata["Poster"].(string)
	if poster == NA {
		return ""
	}
	return poster
}

func (s *OMDB) Map(ctx context.Context, m *models.VideoContent, mt models.ContentType, force bool) (*models.VideoMetadata, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("db is nil")
	}
	otype := om.OmdbTypeMovie
	if mt == models.ContentTypeSeries {
		otype = om.OmdbTypeSeries
	}
	q, err := om.GetQuery(ctx, db, m.Title, m.Year, otype)
	if err != nil {
		return nil, err
	}
	if q != nil && !force {
		if q.ImdbID == nil {
			return nil, nil
		}
		mi, err := om.GetInfoByID(ctx, db, *q.ImdbID)
		if err != nil {
			return nil, err
		}
		if mi == nil {
			return nil, nil
		}

		return NewOmdbMetadata(mi).MakeVideoMetadata(), nil
	}
	omdbType := omdb.OmdbTypeMovie
	if otype == om.OmdbTypeSeries {
		omdbType = omdb.OmdbTypeSeries
	}
	omData, err := s.api.SearchByTitleAndYear(ctx, m.Title, m.Year, omdbType)
	if err != nil {
		return nil, err
	}
	if omData == nil {
		log.Infof("no omdb found for title %v and year %v", m.Title, m.Year)
		_, err = om.InsertQueryIgnoreConflict(ctx, db, m.Title, m.Year, otype, nil)
		if err != nil {
			return nil, err
		}
		return nil, nil
	}
	_, err = om.InsertQueryIgnoreConflict(ctx, db, m.Title, m.Year, otype, &omData.ImdbID)
	if err != nil {
		return nil, err
	}
	omdbInfo, err := om.UpsertInfo(ctx, db, omData.ImdbID, otype, omData.Raw)
	if err != nil {
		return nil, err
	}
	if omdbInfo == nil {
		return nil, nil
	}
	return NewOmdbMetadata(omdbInfo).MakeVideoMetadata(), nil
}

var _ MetadataMapper = (*OMDB)(nil)

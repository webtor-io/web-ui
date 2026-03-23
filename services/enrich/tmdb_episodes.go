package enrich

import (
	"context"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/models"
	tm "github.com/webtor-io/web-ui/models/tmdb"
)

type TMDBEpisodes struct {
	tmdb *TMDB
}

func NewTMDBEpisodes(tmdb *TMDB) *TMDBEpisodes {
	if tmdb == nil {
		return nil
	}
	return &TMDBEpisodes{
		tmdb: tmdb,
	}
}

func (s *TMDBEpisodes) GetName() string {
	return "TMDB Episodes"
}

func (s *TMDBEpisodes) MapEpisodes(ctx context.Context, videoID string, season int, force bool) ([]*models.EpisodeMetadata, error) {
	db := s.tmdb.pg.Get()
	if db == nil {
		return nil, errors.New("db is nil")
	}

	// Resolve TMDB ID from video ID (IMDB ID or tmdbXXX)
	tmdbID, err := s.tmdb.GetTmdbID(ctx, videoID, models.ContentTypeSeries)
	if err != nil {
		return nil, errors.Wrap(err, "failed to resolve tmdb id")
	}
	if tmdbID == 0 {
		log.Infof("no tmdb id found for video id %v", videoID)
		return nil, nil
	}

	// Check cache
	seasonInt16 := int16(season)
	if !force {
		cached, err := tm.GetSeasonInfo(ctx, db, tmdbID, seasonInt16)
		if err != nil {
			return nil, errors.Wrap(err, "failed to get cached season info")
		}
		if cached != nil {
			return s.parseSeasonMetadata(cached.Metadata, videoID), nil
		}
	}

	// Fetch from API
	seasonResp, err := s.tmdb.api.GetTVSeason(ctx, tmdbID, season)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get tv season from tmdb")
	}
	if seasonResp == nil {
		return nil, nil
	}

	// Cache the response
	_, err = tm.UpsertSeasonInfo(ctx, db, tmdbID, seasonInt16, seasonResp.Raw)
	if err != nil {
		return nil, errors.Wrap(err, "failed to cache season info")
	}

	return s.parseSeasonMetadata(seasonResp.Raw, videoID), nil
}

func (s *TMDBEpisodes) parseSeasonMetadata(raw map[string]any, videoID string) []*models.EpisodeMetadata {
	episodesRaw, ok := raw["episodes"].([]any)
	if !ok {
		return nil
	}

	var result []*models.EpisodeMetadata
	for _, epRaw := range episodesRaw {
		epMap, ok := epRaw.(map[string]any)
		if !ok {
			continue
		}

		epNum, _ := epMap["episode_number"].(float64)
		seaNum, _ := epMap["season_number"].(float64)
		name, _ := epMap["name"].(string)
		overview, _ := epMap["overview"].(string)
		airDateStr, _ := epMap["air_date"].(string)
		voteAvg, _ := epMap["vote_average"].(float64)
		stillPath, _ := epMap["still_path"].(string)

		emd := &models.EpisodeMetadata{
			VideoID: videoID,
			Season:  int16(seaNum),
			Episode: int16(epNum),
		}

		if name != "" {
			emd.Title = &name
		}
		if overview != "" {
			emd.Plot = &overview
		}
		if stillPath != "" {
			stillURL := s.tmdb.api.StillURL(stillPath, "original")
			emd.StillURL = &stillURL
		}
		if airDateStr != "" {
			if t, err := time.Parse("2006-01-02", airDateStr); err == nil {
				emd.AirDate = &t
			}
		}
		if voteAvg > 0 {
			emd.Rating = &voteAvg
		}

		result = append(result, emd)
	}

	return result
}

var _ EpisodeMapper = (*TMDBEpisodes)(nil)

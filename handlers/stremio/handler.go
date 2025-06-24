package stremio

import (
	"context"
	"fmt"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/models"
	at "github.com/webtor-io/web-ui/services/access_token"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	"net/http"
	"strconv"
	"strings"
)

const catalogID = "Webtor.io"

const idPrefix = "wt-"

type Handler struct {
	pg     *cs.PG
	at     *at.AccessToken
	domain string
	sapi   *api.Api
}

func RegisterHandler(c *cli.Context, r *gin.Engine, pg *cs.PG, at *at.AccessToken, sapi *api.Api) {
	h := &Handler{
		pg:     pg,
		at:     at,
		domain: c.String("domain"),
		sapi:   sapi,
	}

	gr := r.Group("/stremio")
	gr.GET("/manifest.json", h.manifest)
	gr.Use(auth.HasAuth)
	gr.Use(claims.IsPaid)
	gr.Use(cors.New(cors.Config{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"GET"},
	}))
	gr.POST("/url/generate", h.generateUrl)
	grapi := gr.Group("")
	grapi.Use(at.HasScope("stremio:read"))
	grapi.GET("/catalog/movie/*id", h.movie)
	grapi.GET("/stream/movie/*id", h.streamMovie)
	grapi.GET("/meta/movie/*id", h.metaMovie)
	grapi.GET("/catalog/series/*id", h.series)
	grapi.GET("/stream/series/*id", h.streamSeries)
	grapi.GET("/meta/series/*id", h.metaSeries)

}

func (s *Handler) generateUrl(c *gin.Context) {
	_, err := s.at.Generate(c, "stremio", []string{"stremio:read"})
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.Redirect(http.StatusFound, c.GetHeader("X-Return-Url"))
}

func (s *Handler) manifest(c *gin.Context) {
	m := Manifest{
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
	}
	c.JSON(http.StatusOK, m)
}

func (s *Handler) catalog(c *gin.Context, ct models.ContentType) {
	vcs, err := s.getCatalogData(c, ct)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
	}
	var metas []MetaItem
	for _, vc := range vcs {
		metas = append(metas, s.makeMeta(vc))
	}

	c.JSON(http.StatusOK, &MetasResponse{Metas: metas})
}
func (s *Handler) getCatalogData(c *gin.Context, t models.ContentType) ([]models.VideoContentWithMetadata, error) {
	ctx := c.Request.Context()
	u := auth.GetUserFromContext(c)
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database not initialized")
	}
	var items []models.VideoContentWithMetadata
	if t == models.ContentTypeMovie {
		ls, err := models.GetLibraryMovieList(ctx, db, u.ID, models.SortTypeRecentlyAdded)
		if err != nil {
			return nil, err
		}
		items = make([]models.VideoContentWithMetadata, len(ls))
		for i, v := range ls {
			items[i] = v
		}
	} else if t == models.ContentTypeSeries {
		ls, err := models.GetLibrarySeriesList(ctx, db, u.ID, models.SortTypeRecentlyAdded)
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

func (s *Handler) movie(c *gin.Context) {
	s.catalog(c, models.ContentTypeMovie)
}

func (s *Handler) series(c *gin.Context) {
	s.catalog(c, models.ContentTypeSeries)
}

func (s *Handler) makeStreamURL(c *gin.Context, resourceID string, p string) (string, error) {
	ctx := c.Request.Context()
	cla := api.GetClaimsFromContext(c)
	ti, err := s.retrieveTorrentItem(ctx, resourceID, cla, p)
	if err != nil {
		return "", err
	}
	er, err := s.sapi.ExportResourceContent(ctx, cla, resourceID, ti.ID, "")
	if err != nil {
		return "", err
	}
	return er.ExportItems["download"].URL, nil
}

func (s *Handler) makeStreamTitle(title string, md map[string]any) string {
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

func (s *Handler) streamMovie(c *gin.Context) {
	s.stream(c, models.ContentTypeMovie)
}

func (s *Handler) streamSeries(c *gin.Context) {
	s.stream(c, models.ContentTypeSeries)
}

type Args struct {
	ID      string
	Season  int
	Episode int
}

func (s *Handler) bindArgs(c *gin.Context, ct models.ContentType) (args *Args, err error) {
	id := strings.TrimPrefix(strings.TrimSuffix(c.Param("id"), ".json"), "/")
	if ct == models.ContentTypeMovie {
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

func (s *Handler) getStreamItem(c *gin.Context, vc models.VideoContentWithMetadata, ct models.ContentType, args *Args) (*StreamItem, error) {
	var su, title string
	var err error
	if ct == models.ContentTypeMovie {
		p := *vc.GetPath()
		su, err = s.makeStreamURL(c, vc.GetContent().ResourceID, p)
		if err != nil {
			return nil, err
		}
		title = s.makeStreamTitle(vc.GetContent().Title, vc.GetContent().Metadata)

	} else if ct == models.ContentTypeSeries {
		ep := vc.GetEpisode(args.Season, args.Episode)
		if ep == nil {
			return nil, nil
		}
		p := *ep.Path
		su, err = s.makeStreamURL(c, vc.GetContent().ResourceID, p)
		if err != nil {
			return nil, err
		}

		title = s.makeStreamTitle(fmt.Sprintf("%v.S%vE%v", vc.GetContent().Title, args.Season, args.Episode), ep.Metadata)
	}
	return &StreamItem{
		Title: title,
		Url:   su,
	}, nil

}

func (s *Handler) stream(c *gin.Context, ct models.ContentType) {
	args, err := s.bindArgs(c, ct)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	vcs, err := s.getMetaDataByID(c, ct, args.ID)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	var streams []StreamItem
	for _, vc := range vcs {
		si, err := s.getStreamItem(c, vc, ct, args)

		if err != nil {
			_ = c.AbortWithError(http.StatusInternalServerError, err)
			return
		}

		if si != nil {
			streams = append(streams, *si)
		}

	}
	c.JSON(http.StatusOK, &StreamsResponse{
		Streams: streams,
	})
}

func (s *Handler) getMetaDataByID(c *gin.Context, ct models.ContentType, id string) ([]models.VideoContentWithMetadata, error) {
	isVideoID := true
	if strings.HasPrefix(id, idPrefix) {
		id = strings.TrimPrefix(id, idPrefix)
		isVideoID = false
	}
	ctx := c.Request.Context()
	u := auth.GetUserFromContext(c)
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database not initialized")
	}
	if !isVideoID {
		var vc models.VideoContentWithMetadata
		var err error
		if ct == models.ContentTypeMovie {
			vc, err = models.GetMovieByID(ctx, db, u.ID, id)
		} else if ct == models.ContentTypeSeries {
			vc, err = models.GetSeriesByID(ctx, db, u.ID, id)
		}
		if err != nil {
			return nil, err
		}
		return []models.VideoContentWithMetadata{vc}, nil
	} else {
		var vcs []models.VideoContentWithMetadata
		if ct == models.ContentTypeMovie {
			ls, err := models.GetMoviesByVideoID(ctx, db, u.ID, id)
			if err != nil {
				return nil, err
			}
			for _, l := range ls {
				vcs = append(vcs, l)
			}
		} else if ct == models.ContentTypeSeries {
			ls, err := models.GetSeriesByVideoID(ctx, db, u.ID, id)
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

func (s *Handler) makePoster(vc models.VideoContentWithMetadata) string {
	return fmt.Sprintf("%v/lib/%v/poster/%v/240.jpg", s.domain, vc.GetContentType(), vc.GetMetadata().VideoID)
}

func (s *Handler) makeMeta(vc models.VideoContentWithMetadata) MetaItem {
	if vc.GetMetadata() != nil {
		return s.makeMetaWithMetadata(vc)
	} else {
		return s.makeMetaWithoutMetadata(vc)
	}

}
func (s *Handler) makeMetaWithMetadata(vc models.VideoContentWithMetadata) MetaItem {
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

func (s *Handler) makeMetaWithoutMetadata(vc models.VideoContentWithMetadata) MetaItem {
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

func (s *Handler) meta(c *gin.Context, ct models.ContentType) {
	args, err := s.bindArgs(c, ct)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	vcs, err := s.getMetaDataByID(c, ct, args.ID)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	if vcs == nil && len(vcs) == 0 {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	vc := vcs[0]
	meta := s.makeMeta(vc)
	if ct == models.ContentTypeSeries {
		meta.Videos, err = s.makeVideos(vc)
	}
	c.JSON(http.StatusOK, &MetaResponse{Meta: meta})
}

func (s *Handler) metaMovie(c *gin.Context) {
	s.meta(c, models.ContentTypeMovie)
}

func (s *Handler) metaSeries(c *gin.Context) {
	s.meta(c, models.ContentTypeSeries)
}

func (s *Handler) retrieveTorrentItem(ctx context.Context, hash string, claims *api.Claims, path string) (*ra.ListItem, error) {
	limit := uint(100)
	offset := uint(0)
	for {
		resp, err := s.sapi.ListResourceContent(ctx, claims, hash, &api.ListResourceContentArgs{
			Limit:  limit,
			Offset: offset,
		})
		if err != nil {
			return nil, err
		}
		for _, item := range resp.Items {
			if item.PathStr == path {
				return &item, nil
			}
		}
		if (resp.Count - int(offset)) == len(resp.Items) {
			break
		}
		offset += limit
	}
	return nil, nil
}

func (s *Handler) makeVideos(vc models.VideoContentWithMetadata) ([]VideoItem, error) {
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

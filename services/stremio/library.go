package stremio

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
)

const idPrefix = "wt-"

type Library struct {
	db     *pg.DB
	u      *auth.User
	domain string
	sapi   *api.Api
	cla    *api.Claims
}

func NewLibrary(domain string, db *pg.DB, u *auth.User, sapi *api.Api, cla *api.Claims) *Library {
	return &Library{
		db:     db,
		u:      u,
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
		ls, err := models.GetLibraryMovieList(ctx, s.db, s.u.ID, models.SortTypeRecentlyAdded, "")
		if err != nil {
			return nil, err
		}
		items = make([]models.VideoContentWithMetadata, len(ls))
		for i, v := range ls {
			items[i] = v
		}
	} else if t == "series" {
		ls, err := models.GetLibrarySeriesList(ctx, s.db, s.u.ID, models.SortTypeRecentlyAdded, "")
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
	if len(parts) > 2 {
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
	var title, p string
	var md map[string]any
	var fileIdx *int
	var fileSize *int64
	if ct == "movie" {
		p = *vc.GetPath()
		title = vc.GetContent().Title
		md = vc.GetContent().Metadata
		fileIdx = vc.GetFileIdx()
		fileSize = vc.GetFileSize()

	} else if ct == "series" {
		ep := vc.GetEpisode(args.Season, args.Episode)
		if ep == nil {
			return nil, nil
		}
		p = *ep.Path
		title = fmt.Sprintf("%v.S%vE%v", vc.GetContent().Title, args.Season, args.Episode)
		md = ep.Metadata
		fileIdx = ep.FileIdx
		fileSize = ep.FileSize
	}

	idx, filename, err := s.resolveFileItem(ctx, vc.GetContent().ResourceID, p, fileIdx)
	if err != nil {
		return nil, err
	}
	// Path no longer present in the torrent listing (re-packed / mismatch):
	// skip the stream rather than emit a broken one.
	if filename == "" {
		return nil, nil
	}
	return &StreamItem{
		Name:        s.makeStreamName("Webtor.io", md),
		Title:       s.makeStreamTitle(title, md),
		Description: makeStreamDescription(vc.GetContent().Title, ct, args, md, fileSize, filename, vc.GetContent().Year),
		InfoHash:    vc.GetContent().ResourceID,
		FileIdx:     idx,
		BehaviorHints: &StreamBehaviorHints{
			Filename:   filename,
			BingeGroup: fmt.Sprintf("webtorio|%v", vc.GetContent().ResourceID),
		},
	}, nil

}

// makeStreamDescription builds the Torrentio-style multi-line stream
// description shown by Stremio: a clean release line, a size/source line, and
// an optional language-flag line. Mirrors how addon streams present their
// detail block so library entries don't look bare next to them. Every part is
// optional — missing metadata simply drops its segment.
//
//	The Big Bang Theory · S05E14 [2012 BluRay 1080p]
//	💾 1.41 GB  ⚙️ Library
//	🇬🇧 / 🇷🇺
func makeStreamDescription(displayTitle, ct string, args *Args, md map[string]any, size *int64, filename string, year *int16) string {
	line1 := displayTitle
	if ct == "series" {
		line1 = fmt.Sprintf("%s · S%02dE%02d", displayTitle, args.Season, args.Episode)
	}
	var tags []string
	if y := mdYear(md, year); y != "" {
		tags = append(tags, y)
	}
	if q := mdString(md, "quality"); q != "" {
		tags = append(tags, q)
	}
	if r := mdString(md, "resolution"); r != "" {
		tags = append(tags, r)
	}
	if len(tags) > 0 {
		line1 += " [" + strings.Join(tags, " ") + "]"
	}

	line2 := []string{}
	if size != nil && *size > 0 {
		line2 = append(line2, "💾 "+humanizeBytes(*size))
	}
	line2 = append(line2, "⚙️ Library")

	lines := []string{line1, strings.Join(line2, "  ")}
	if flags := languageFlags(filename); flags != "" {
		lines = append(lines, flags)
	}
	return strings.Join(lines, "\n")
}

// mdString returns a trimmed string value from the ptn metadata snapshot, or
// "" when absent or not a string.
func mdString(md map[string]any, key string) string {
	if md == nil {
		return ""
	}
	if v, ok := md[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// mdYear reads the parsed year (a JSON number → float64 in the snapshot),
// falling back to the content-level year. Returns "" when neither is set.
func mdYear(md map[string]any, fallback *int16) string {
	if md != nil {
		switch v := md["year"].(type) {
		case float64:
			if v > 0 {
				return strconv.Itoa(int(v))
			}
		case string:
			if strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
	}
	if fallback != nil && *fallback > 0 {
		return strconv.Itoa(int(*fallback))
	}
	return ""
}

// languageFlags extracts language tokens from the filename and renders their
// flag emoji, de-duplicated and joined " / " (e.g. "🇬🇧 / 🇷🇺"). Returns ""
// when no language is recognised.
func languageFlags(s string) string {
	langs := ExtractLanguages(s)
	if len(langs) == 0 {
		return ""
	}
	seen := map[string]bool{}
	var flags []string
	for _, l := range langs {
		if l.Flag == "" || seen[l.Flag] {
			continue
		}
		seen[l.Flag] = true
		flags = append(flags, l.Flag)
	}
	return strings.Join(flags, " / ")
}

// humanizeBytes renders a byte count as a human-readable size (e.g.
// "1.41 GB", "720 MB"). Binary units, two significant decimals above MB.
func humanizeBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	value := float64(n) / float64(div)
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	if value >= 100 {
		return fmt.Sprintf("%.0f %s", value, units[exp])
	}
	return fmt.Sprintf("%.2f %s", value, units[exp])
}

// resolveFileItem returns the torrent file index and filename for a library
// entry. When fileIdx was persisted at enrich time (the common case) it is a
// pure in-memory resolution — filename is the path basename — and avoids the
// rest-api /list pagination that used to run on every /stream request and,
// under the CompositeStream 5s timeout, intermittently dropped the whole
// Library result. Rows enriched before the file_idx column existed
// (fileIdx == nil) fall back to the list walk.
func (s *Library) resolveFileItem(ctx context.Context, hash, p string, fileIdx *int) (int, string, error) {
	if fileIdx != nil {
		return *fileIdx, path.Base(p), nil
	}
	ti, idx, err := s.retrieveTorrentItem(ctx, hash, s.cla, p)
	if err != nil {
		return 0, "", err
	}
	if ti == nil {
		return 0, "", nil
	}
	return idx, ti.Name, nil
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
			vc, err = models.GetMovieByID(ctx, s.db, s.u.ID, id)
		} else if ct == "series" {
			vc, err = models.GetSeriesByID(ctx, s.db, s.u.ID, id)
		}
		if err != nil {
			return nil, err
		}
		return []models.VideoContentWithMetadata{vc}, nil
	} else {
		var vcs []models.VideoContentWithMetadata
		if ct == "movie" {
			ls, err := models.GetMoviesByVideoID(ctx, s.db, s.u.ID, id)
			if err != nil {
				return nil, err
			}
			for _, l := range ls {
				vcs = append(vcs, l)
			}
		} else if ct == "series" {
			ls, err := models.GetSeriesByVideoID(ctx, s.db, s.u.ID, id)
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

// makePoster returns the resource-keyed unified poster URL. Same
// endpoint as the library/continue-watching cards on the web — IMDb
// poster resolves first, per-resource thumbnail falls back, 404 if
// neither. Stremio renders its own placeholder on 404.
func (s *Library) makePoster(vc models.VideoContentWithMetadata) string {
	if vc.GetContent() == nil || vc.GetContent().ResourceID == "" {
		return ""
	}
	return fmt.Sprintf("%v/lib/poster/%v/240.jpg", s.domain, vc.GetContent().ResourceID)
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
	// Always emit the resource-keyed poster URL — endpoint resolves
	// IMDb → thumbnail → 404 internally. Even if PosterURL on the
	// metadata is empty, the per-resource thumbnail (generated at
	// stream/download) can still cover it.
	meta.Poster = s.makePoster(vc)
	return meta
}

func (s *Library) makeMetaWithoutMetadata(vc models.VideoContentWithMetadata) MetaItem {
	meta := MetaItem{
		ID:          idPrefix + vc.GetID().String(),
		Type:        string(vc.GetContentType()),
		Name:        vc.GetContent().Title,
		PosterShape: "poster",
		// Un-enriched torrents still have a resource_id; the unified
		// endpoint serves the generated thumbnail when available,
		// 404s otherwise. Stremio renders its own placeholder on 404.
		Poster: s.makePoster(vc),
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
		name := fmt.Sprintf("Episode %d", ep)
		if e.EpisodeMetadata != nil && e.EpisodeMetadata.Title != nil && *e.EpisodeMetadata.Title != "" {
			name = *e.EpisodeMetadata.Title
		}
		vi := VideoItem{
			Name:    name,
			ID:      fmt.Sprintf("%v:%v:%v", id, sea, ep),
			Season:  sea,
			Episode: ep,
		}
		vis = append(vis, vi)
	}
	return vis, nil
}

func (s *Library) makeStreamName(name string, md map[string]any) string {
	if resolution, ok := md["resolution"]; ok && strings.TrimSpace(resolution.(string)) != "" {
		name = name + "\n" + resolution.(string)
		return name
	}
	if quality, ok := md["quality"]; ok && strings.TrimSpace(quality.(string)) != "" {
		name = name + "\n" + quality.(string)
		return name
	}
	return name
}

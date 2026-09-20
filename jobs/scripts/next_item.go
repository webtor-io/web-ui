package scripts

import (
	"context"
	"fmt"
	"path"
	"time"

	log "github.com/sirupsen/logrus"
	ra "github.com/webtor-io/rest-api/services"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/next_item"
)

// NextItem is what the player needs to offer and start the file that follows
// this one (docs/superpowers/specs/2026-09-20-next-episode-design.md). Absent
// from the render = no next file, or the lookup failed: the feature is simply
// not there for this stream, which is also its kill switch.
type NextItem struct {
	ItemID string
	Path   string
	Kind   string // "episode" | "track"
	Label  string // "S01E03 · Title" / track name
}

// nextItemTimeout bounds the whole lookup: it rides on the stream job, and a
// slow listing must not hold the player up for a convenience.
const nextItemTimeout = 4 * time.Second

// maxSiblings is how much of a directory is listed to find the next track.
// An album is dozens of files; an audiobook can be hundreds.
const maxSiblings = 1000

func (s *ActionScript) resolveNextItem(ctx context.Context, claims *api.Claims, resourceID string, cur *ra.ListItem) *NextItem {
	if cur == nil || cur.PathStr == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, nextItemTimeout)
	defer cancel()

	var pick *next_item.Pick
	switch cur.MediaFormat {
	case ra.Video:
		pick = next_item.NextEpisode(cur.PathStr, s.siblingEpisodes(ctx, resourceID, cur.PathStr))
	case ra.Audio:
		l, err := s.api.ListResourceContentCached(ctx, claims, resourceID, &api.ListResourceContentArgs{
			Path:  path.Dir(cur.PathStr),
			Limit: maxSiblings,
		})
		if err != nil || l == nil {
			log.WithError(err).WithField("resource_id", resourceID).Warn("next item: failed to list the directory")
			return nil
		}
		files := make([]next_item.File, 0, len(l.Items))
		for _, it := range l.Items {
			if it.Type != ra.ListTypeFile {
				continue
			}
			files = append(files, next_item.File{ID: it.ID, Path: it.PathStr, Name: it.Name, Audio: it.MediaFormat == ra.Audio})
		}
		pick = next_item.NextTrack(cur.PathStr, files)
	}
	if pick == nil {
		return nil
	}
	// The item id is rest-api's, so the file is asked for by path -- the same
	// way the resource page finds the file it was opened with (getBestItem).
	l, err := s.api.ListResourceContentCached(ctx, claims, resourceID, &api.ListResourceContentArgs{Path: pick.Path})
	if err != nil || l == nil || len(l.Items) == 0 || l.Items[0].PathStr != pick.Path {
		log.WithError(err).WithField("resource_id", resourceID).WithField("path", pick.Path).Warn("next item: the picked file is not in the listing")
		return nil
	}
	return &NextItem{ItemID: l.Items[0].ID, Path: pick.Path, Kind: string(pick.Kind), Label: nextItemLabel(pick)}
}

func (s *ActionScript) siblingEpisodes(ctx context.Context, resourceID string, p string) []next_item.Episode {
	if s.enricher == nil {
		return nil
	}
	rows, err := s.enricher.SiblingEpisodes(ctx, resourceID, p)
	if err != nil {
		log.WithError(err).WithField("resource_id", resourceID).Warn("next item: failed to load episodes")
		return nil
	}
	return episodesForPick(rows)
}

// episodesForPick keeps the rows next_item can order: both numbers known.
func episodesForPick(rows []*models.Episode) []next_item.Episode {
	out := make([]next_item.Episode, 0, len(rows))
	for _, e := range rows {
		if e == nil || e.Season == nil || e.Episode == nil {
			continue
		}
		ep := next_item.Episode{Season: int(*e.Season), Episode: int(*e.Episode)}
		if e.Path != nil {
			ep.Path = *e.Path
		}
		if e.Title != nil {
			ep.Title = *e.Title
		}
		out = append(out, ep)
	}
	return out
}

func nextItemLabel(p *next_item.Pick) string {
	if p.Kind != next_item.KindEpisode {
		return p.Title
	}
	tag := fmt.Sprintf("S%02dE%02d", p.Season, p.Episode)
	if p.Title == "" {
		return tag
	}
	return tag + " · " + p.Title
}

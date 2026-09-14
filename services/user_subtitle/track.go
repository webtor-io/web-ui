package user_subtitle

import (
	"github.com/webtor-io/web-ui/models"
)

// Tracks maps stored uploads onto the render-ready shape the picker
// consumes. Both entry points go through it — the initial page render
// (jobs/scripts.streamContent) and the async reload after an upload or a
// delete (handlers/user_subtitle.buildView) — because a field only one of
// them fills is a field the page silently loses on reload.
//
// SrcLang was exactly that: the async reload derived it from the filename,
// the initial render left it empty, so an upload that sat under its own
// language chip before F5 moved to the "Unknown" group after it. Anything
// per-render (Selected, Default, Saved) is set by the caller on top.
//
// src wraps the stored blob into a URL the player can load. It may be nil
// when there is no export URL to hang the subtitle off — the list still
// renders, just without playable sources.
func Tracks(list []*models.UserSubtitle, src func(*models.UserSubtitle) string) []models.UserSubtitleTrack {
	tracks := make([]models.UserSubtitleTrack, 0, len(list))
	for _, sub := range list {
		var wrapped string
		if src != nil {
			wrapped = src(sub)
		}
		tracks = append(tracks, models.UserSubtitleTrack{
			ID:           TrackID(sub.UserSubtitleID),
			Src:          wrapped,
			Label:        sub.OriginalName,
			Format:       sub.Format,
			Size:         sub.Size,
			OriginalName: sub.OriginalName,
			SrcLang:      LangFromName(sub.OriginalName),
			DeleteURL:    DeleteURL(sub.UserSubtitleID),
		})
	}
	return tracks
}

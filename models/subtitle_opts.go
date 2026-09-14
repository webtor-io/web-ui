package models

// SubtitleOpts carries the viewer-specific inputs of the subtitle ladder (see docs/subtitle_translate.md).
type SubtitleOpts struct {
	// PreferredLang is the viewer's language, canonized to a base tag.
	PreferredLang string
	// Translate gates whether AI translation is offered at all.
	Translate bool
	// Paid marks whether the viewer's tier may activate a translated track.
	Paid bool
	// Names are the file/library display names considered for NSFW gating.
	Names []string
}

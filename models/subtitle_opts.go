package models

// SubtitleOpts carries the viewer-specific inputs of the subtitle ladder (see docs/subtitle_translate.md).
type SubtitleOpts struct {
	// PreferredLang is the viewer's language, canonized to a base tag.
	PreferredLang string
	// Translate gates whether AI translation is offered at all.
	Translate bool
	// Paid marks whether the viewer's tier may activate a translated track.
	Paid bool
	// Names is the glossary passed to the translator (cast names from TMDB credits).
	Names []string
	// HLSSessionBase is the transcoder session's URL prefix
	// (…~hls/session/<id>, query included) while the stream plays through
	// the transcoder; empty otherwise. Embedded subtitle tracks get their
	// playlist Src from it, so they can feed the translation chain.
	HLSSessionBase string
}

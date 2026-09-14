package stremio

import "strings"

// LangDisplay is everything a template needs to show one language on a
// picker chip. Known languages get a flag and a name from Languages;
// everything else gets a bare uppercase code, which is deliberately not
// localized — like an ISO tag or the EM/IN/OS origin codes, it is a code.
//
// Lang is the base tag the picker groups by ("pt" for both "por" and
// "pt-BR") and is never empty: an unknown or missing tag groups under
// "und", so every track belongs to exactly one language chip.
type LangDisplay struct {
	Lang string
	Name string
	Flag string
	Code string
}

// NewLangDisplay resolves a subtitle/audio track's srclang. The tag is
// already canonical by the time it gets here (Helper.canonizeSrcLangs in
// handlers/action runs golang.org/x/text over it), so the base language is
// the part before the first separator — the same rule baseLang() applies on
// the client (assets/src/js/lib/player/subtitle-rules.js), which is what
// keeps server-rendered groups and client-recomputed groups agreeing.
func NewLangDisplay(tag string) LangDisplay {
	base := strings.TrimSpace(tag)
	base = strings.ReplaceAll(base, "_", "-")
	base = strings.ToLower(strings.SplitN(base, "-", 2)[0])
	if base == "" || base == "und" {
		return LangDisplay{Lang: "und", Code: "UND"}
	}
	if l := LanguageByCode(base); l != nil {
		return LangDisplay{Lang: base, Name: l.Name, Flag: l.Flag}
	}
	return LangDisplay{Lang: base, Code: strings.ToUpper(base)}
}

// LangDisplay exposes NewLangDisplay to Go HTML templates. Registered
// globally via template.Manager.WithHelper (serve.go), so the uploads
// partial — rendered by the user_subtitle builder, which does not carry
// handlers/action.Helper — can call it too.
//
// Template usage: {{ $d := langDisplay .SrcLang }}{{ $d.Flag }} {{ $d.Name }}
func (s *Helper) LangDisplay(tag string) LangDisplay {
	return NewLangDisplay(tag)
}

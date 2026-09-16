package stremio

import (
	"strings"

	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

// LangDisplay is everything a template needs to show one language on a
// picker chip. Known languages get a flag and a name from Languages;
// everything else gets a bare uppercase code, which is deliberately not
// localized — like an ISO tag or the EM/IN/OS origin codes, it is a code.
//
// Lang is the base tag the picker groups by ("pt" for both "por" and
// "pt-BR") and is never empty: an unknown or missing tag groups under
// "und", so every track belongs to exactly one language chip.
//
// Localized is the language's own name in a UI locale — set by
// NewLangDisplayIn/LangDisplayIn for the two picker sentences that
// interpolate a language name into UI copy ("Translate to {{.Language}}").
// NewLangDisplay/LangDisplay (the chip path) set it equal to Name, English:
// chips are not sentences, they stay Discover-style regardless of $.Lang.
type LangDisplay struct {
	Lang      string
	Name      string
	Flag      string
	Code      string
	Localized string
}

// NewLangDisplay resolves a subtitle/audio track's srclang. The tag is
// already canonical by the time it gets here (Helper.canonizeSrcLangs in
// handlers/action runs golang.org/x/text over it), so the base language is
// the part before the first separator — the same rule baseLang() applies on
// the client (assets/src/js/lib/player/track-picker.js), which is what
// keeps server-rendered groups and client-recomputed groups agreeing.
func NewLangDisplay(tag string) LangDisplay {
	base := strings.TrimSpace(tag)
	base = strings.ReplaceAll(base, "_", "-")
	base = strings.ToLower(strings.SplitN(base, "-", 2)[0])
	if base == "" || base == "und" {
		return LangDisplay{Lang: "und", Code: "UND"}
	}
	if l := LanguageByCode(base); l != nil {
		return LangDisplay{Lang: base, Name: l.Name, Flag: l.Flag, Localized: l.Name}
	}
	return LangDisplay{Lang: base, Code: strings.ToUpper(base)}
}

// NewLangDisplayIn is NewLangDisplay plus Localized resolved into uiLang:
// the language's own name in the UI's locale, from
// golang.org/x/text/language/display — "немецкий" for German when uiLang is
// "ru", instead of the English "German" the plain chip path uses.
//
// GUARD, load-bearing: display.Tags(t) returns a NIL Namer when t has no
// usable match (verified against the x/text source, language/display.go —
// it happens exactly when uiLang parses to `und`, which is what
// language.Make returns for "" and for anything it cannot parse, e.g. a
// garbage locale code; language.Make itself never panics, it just falls
// back to `und`). Calling .Name() on that nil interface panics, so the nil
// check below must run before every call — see the negative control in
// lang_display_test.go (TestNewLangDisplayIn/garbage_uiLang and
// /und_uiLang), which reddens (panics) if this guard is removed.
//
// A tag NewLangDisplay itself could not name (und, or parseable-but-unlisted
// like "is") has no English Name either, and Localized stays "" for it too
// — there is nothing to localize, and the chip for the same tag shows only
// a bare Code, never a name in any language.
func NewLangDisplayIn(uiLang, tag string) LangDisplay {
	d := NewLangDisplay(tag)
	if d.Name == "" {
		return d
	}
	if namer := display.Tags(language.Make(uiLang)); namer != nil {
		if loc := namer.Name(language.Make(d.Lang)); loc != "" {
			d.Localized = loc
		}
	}
	return d
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

// LangDisplayIn exposes NewLangDisplayIn to templates as langDisplayIn.
// Registered the same way as LangDisplay (template.Manager.WithHelper binds
// every exported method by reflection regardless of arity, so a two-arg
// method registers exactly like a one-arg one).
//
// Template usage: {{ (langDisplayIn $.Lang .SrcLang).Localized }}
func (s *Helper) LangDisplayIn(uiLang, tag string) LangDisplay {
	return NewLangDisplayIn(uiLang, tag)
}

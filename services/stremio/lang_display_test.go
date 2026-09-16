package stremio

import "testing"

func TestNewLangDisplay(t *testing.T) {
	cases := []struct {
		in                     string
		lang, name, flag, code string
	}{
		{"ru", "ru", "Russian", "🇷🇺", ""},
		{"pt-BR", "pt", "Portuguese", "🇧🇷", ""},
		{"pt_BR", "pt", "Portuguese", "🇧🇷", ""},
		{"EN", "en", "English", "🇬🇧", ""},
		// Разбираемый, но не входящий в таблицу тег: группировать по нему
		// можно, показывать — только как код.
		// "is" (Icelandic) was "ka" until Georgian joined the table on
		// 2026-09-16; any tag outside Languages does.
		{"is", "is", "", "", "IS"},
		{"und", "und", "", "", "UND"},
		{"", "und", "", "", "UND"},
		{"  ", "und", "", "", "UND"},
	}
	for _, c := range cases {
		got := NewLangDisplay(c.in)
		if got.Lang != c.lang || got.Name != c.name || got.Flag != c.flag || got.Code != c.code {
			t.Errorf("NewLangDisplay(%q) = %+v, want {%q %q %q %q}",
				c.in, got, c.lang, c.name, c.flag, c.code)
		}
	}
}

// NewLangDisplay is a template-facing constructor too (chips call it
// directly), so its Localized must already equal the English Name — the
// picker's two sentences read .Localized unconditionally, and a chip
// context has no UI locale to resolve into.
func TestNewLangDisplayLocalizedDefaultsToName(t *testing.T) {
	got := NewLangDisplay("de")
	if got.Localized != "German" {
		t.Errorf("NewLangDisplay(%q).Localized = %q, want %q", "de", got.Localized, "German")
	}
}

func TestNewLangDisplayIn(t *testing.T) {
	cases := []struct {
		name         string
		uiLang, tag  string
		wantLocalize string
	}{
		// ru UI, German track: the language's own name in Russian.
		{"ru/de", "ru", "de", "немецкий"},
		// en UI, German track: CLDR's English name matches our own table.
		{"en/de", "en", "de", "German"},
		// de UI, German track: the language names itself.
		{"de/de", "de", "de", "Deutsch"},
		// Garbage/unmatched uiLang ("xx-??" parses to `und` —
		// language.Make never panics, it falls back to the zero Tag):
		// display.Tags(und) returns a NIL Namer (verified against the
		// x/text source, language/display/display.go: Tags returns nil
		// when matcher.Match reports language.No confidence, which is
		// exactly what an `und` want-tag gets). Calling .Name() on a nil
		// Namer panics, so NewLangDisplayIn must guard it and fall back
		// to the English Name instead of ever making that call.
		{"garbage uiLang", "xx-??", "de", "German"},
		{"und uiLang", "und", "de", "German"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := NewLangDisplayIn(c.uiLang, c.tag)
			if got.Localized != c.wantLocalize {
				t.Errorf("NewLangDisplayIn(%q, %q).Localized = %q, want %q",
					c.uiLang, c.tag, got.Localized, c.wantLocalize)
			}
		})
	}
}

// A tag NewLangDisplay itself cannot name (und, or a parseable-but-unlisted
// code) has no English Name to localize either way: Localized must stay ""
// rather than surface a CLDR translation our own chip never shows (that
// chip would read the bare Code, e.g. "IS", not a name in any language).
func TestNewLangDisplayInUnknownStaysEmpty(t *testing.T) {
	cases := []struct{ uiLang, tag string }{
		{"ru", "und"},
		{"ru", ""},
		{"ru", "is"}, // valid ISO code, not in our own Languages table
	}
	for _, c := range cases {
		got := NewLangDisplayIn(c.uiLang, c.tag)
		if got.Localized != "" {
			t.Errorf("NewLangDisplayIn(%q, %q).Localized = %q, want \"\"", c.uiLang, c.tag, got.Localized)
		}
	}
}

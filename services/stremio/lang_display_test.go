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
		{"ka", "ka", "", "", "KA"},
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

package i18n

import (
	"encoding/json"
	"io/fs"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	log "github.com/sirupsen/logrus"
	"golang.org/x/text/language"
)

var SupportedLangs = []string{"en", "ru", "es", "de", "fr", "pt", "it"}

var DefaultLang = "en"

// LangNames maps language codes to their native display names.
//
// Note: "pt" carries Brazilian Portuguese (PT-BR) content. We use the bare
// two-letter code for URL/middleware simplicity — see middleware.go:69
// which assumes 2-char prefixes. If/when we add European Portuguese, split
// this into "pt-br" and "pt-pt" and teach the middleware about variable-
// length prefixes.
var LangNames = map[string]string{
	"en": "English",
	"ru": "Русский",
	"es": "Español",
	"de": "Deutsch",
	"fr": "Français",
	"pt": "Português",
	"it": "Italiano",
}

type Service struct {
	Bundle *i18n.Bundle
}

func New(localeFS fs.FS) *Service {
	bundle := i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("json", json.Unmarshal)
	for _, lang := range SupportedLangs {
		_, err := bundle.LoadMessageFileFS(localeFS, lang+".json")
		if err != nil {
			log.WithError(err).WithField("lang", lang).Error("failed to load locale file")
		}
	}
	return &Service{Bundle: bundle}
}

func (s *Service) Localizer(lang string) *i18n.Localizer {
	return i18n.NewLocalizer(s.Bundle, lang)
}

func IsSupported(lang string) bool {
	for _, l := range SupportedLangs {
		if l == lang {
			return true
		}
	}
	return false
}

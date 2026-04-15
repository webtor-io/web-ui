package i18n

import (
	"encoding/json"
	"io/fs"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	log "github.com/sirupsen/logrus"
	"golang.org/x/text/language"
)

var SupportedLangs = []string{"en", "ru", "es", "de"}

var DefaultLang = "en"

// LangNames maps language codes to their native display names.
var LangNames = map[string]string{
	"en": "English",
	"ru": "Русский",
	"es": "Español",
	"de": "Deutsch",
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

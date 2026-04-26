package i18n

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	log "github.com/sirupsen/logrus"
	"golang.org/x/text/language"
)

// SupportedLangs is the list of locales the app supports, in language-switcher
// display order (EN first, then alphabetical by code).
//
// Populated by New() from the locale FS — adding a `xx.json` file under
// `locales/` is enough for both server and the language switcher to pick
// it up. Stays empty until New() runs; consumers that need it before that
// (notably tests in other packages) should call New() in TestMain.
var SupportedLangs []string

// DefaultLang is the canonical English code. Used for "no URL prefix" and
// fallback cases throughout the i18n stack.
var DefaultLang = "en"

// LangPath prefixes a path with the language code if it isn't the default.
// Mirrors the template helper `langPath` (services/web/helper.go) so handlers
// can build lang-aware redirect targets without going through templates.
// Empty or default lang returns the path unchanged.
func LangPath(lang, path string) string {
	if lang == "" || lang == DefaultLang {
		return path
	}
	return "/" + lang + path
}

// LangNames maps a 2-letter locale code to its native display name. This is
// the one piece of per-locale metadata that cannot be derived from the
// filesystem — when adding a new locale, add an entry here too. New() will
// fail loudly at startup if a locale file has no LangNames entry.
//
// Note: "pt" carries Brazilian Portuguese (PT-BR) content. We use the bare
// two-letter code for URL/middleware simplicity — see middleware.go which
// assumes 2-char prefixes. If/when we add European Portuguese, split this
// into "pt-br" and "pt-pt" and teach the middleware about variable-length
// prefixes.
var LangNames = map[string]string{
	"en": "English",
	"ru": "Русский",
	"es": "Español",
	"de": "Deutsch",
	"fr": "Français",
	"pt": "Português",
	"it": "Italiano",
	"pl": "Polski",
	"tr": "Türkçe",
	"nl": "Nederlands",
	"cs": "Čeština",
}

type Service struct {
	Bundle *i18n.Bundle
}

var globalService *Service

// GlobalService returns the singleton Service instance created by New().
// Panics if called before New().
func GlobalService() *Service {
	if globalService == nil {
		panic("i18n: GlobalService() called before New()")
	}
	return globalService
}

// New scans localeFS for `xx.json` files (2-letter codes only), populates
// the package-level SupportedLangs, and loads each one into the i18n bundle.
// Calling New is the single point that wires the supported-locales list at
// runtime; consumers that read SupportedLangs do so AFTER New runs.
//
// Panics on programming errors (locale file present but missing LangNames
// entry) — these would otherwise produce confusing UI gaps that are hard
// to track down post-deploy.
func New(localeFS fs.FS) *Service {
	SupportedLangs = discoverLocales(localeFS)

	bundle := i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("json", unmarshalStrippingAtKeys)
	for _, lang := range SupportedLangs {
		_, err := bundle.LoadMessageFileFS(localeFS, lang+".json")
		if err != nil {
			log.WithError(err).WithField("lang", lang).Error("failed to load locale file")
		}
	}
	svc := &Service{Bundle: bundle}
	globalService = svc
	return svc
}

// discoverLocales lists files in the root of localeFS, keeps only 2-letter
// `xx.json` names, validates each has a LangNames entry, and returns codes
// in switcher display order: DefaultLang first, then the rest sorted
// alphabetically.
func discoverLocales(localeFS fs.FS) []string {
	entries, err := fs.ReadDir(localeFS, ".")
	if err != nil {
		log.WithError(err).Error("failed to read locales directory")
		return nil
	}
	var codes []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		code := strings.TrimSuffix(name, ".json")
		// Restrict to 2-letter codes — guards against backups (en.json.bak),
		// drafts named in full (`portuguese.json`), or future variant codes
		// that the URL middleware doesn't yet support.
		if len(code) != 2 {
			log.WithField("file", name).Warn("ignoring non-2-letter locale file")
			continue
		}
		if _, ok := LangNames[code]; !ok {
			panic(fmt.Sprintf(
				"i18n: locales/%s.json has no entry in LangNames map "+
					"(services/i18n/i18n.go) — add the native display name "+
					"alongside the translation file",
				code))
		}
		codes = append(codes, code)
	}
	sort.Strings(codes)

	// EN first, others alphabetical — matches historical switcher order
	// where the default language leads the dropdown.
	out := make([]string, 0, len(codes))
	for _, c := range codes {
		if c == DefaultLang {
			out = append(out, c)
			break
		}
	}
	for _, c := range codes {
		if c != DefaultLang {
			out = append(out, c)
		}
	}
	return out
}

// unmarshalStrippingAtKeys decodes a locale JSON file and removes any keys
// whose names start with "@". Those keys carry translator context (ARB-style
// metadata, e.g. "@support.work": "Title of the copyrighted creative work…")
// and must not be registered as translatable messages with the i18n bundle.
//
// go-i18n's loader (ParseMessageFileBytes) passes v as *interface{}; after
// json.Unmarshal the underlying value is a map[string]interface{} for a JSON
// object locale file. We mutate that map in place to drop "@" keys.
func unmarshalStrippingAtKeys(data []byte, v any) error {
	if err := json.Unmarshal(data, v); err != nil {
		return err
	}
	ptr, ok := v.(*interface{})
	if !ok {
		return nil
	}
	m, ok := (*ptr).(map[string]interface{})
	if !ok {
		return nil
	}
	for k := range m {
		if strings.HasPrefix(k, "@") {
			delete(m, k)
		}
	}
	return nil
}

func (s *Service) Localizer(lang string) *i18n.Localizer {
	return i18n.NewLocalizer(s.Bundle, lang)
}

// IsSupported reports whether the given lang code is in SupportedLangs.
// Returns false if New has not yet been called (SupportedLangs is empty).
func IsSupported(lang string) bool {
	for _, l := range SupportedLangs {
		if l == lang {
			return true
		}
	}
	return false
}

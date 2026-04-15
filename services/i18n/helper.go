package i18n

import (
	"fmt"
	"html/template"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	log "github.com/sirupsen/logrus"
)

// TranslateWithLocalizer translates a key using the given Localizer.
// Returns the key itself if translation is missing.
func TranslateWithLocalizer(loc *i18n.Localizer, key string) string {
	if loc == nil {
		return key
	}
	msg, err := loc.Localize(&i18n.LocalizeConfig{MessageID: key})
	if err != nil {
		return key
	}
	return msg
}

// TranslateWithLocalizerData translates a parameterized key using the given
// Localizer and template data. Use for messages like `{{.Bytes}}` / `{{.Peers}}`.
// Returns the key itself if translation is missing.
func TranslateWithLocalizerData(loc *i18n.Localizer, key string, data map[string]any) string {
	if loc == nil {
		return key
	}
	msg, err := loc.Localize(&i18n.LocalizeConfig{
		MessageID:    key,
		TemplateData: data,
	})
	if err != nil {
		return key
	}
	return msg
}

// Helper exposes i18n translation functions as template helpers.
// Registered via template.Manager.WithHelper().
type Helper struct {
	svc *Service
}

func NewHelper(svc *Service) *Helper {
	return &Helper{svc: svc}
}

// T translates a message by key for the given language.
// Template usage: {{ t $.Lang "nav.discover" }}
func (h *Helper) T(lang string, key string) string {
	loc := h.svc.Localizer(lang)
	msg, err := loc.Localize(&i18n.LocalizeConfig{MessageID: key})
	if err != nil {
		log.WithError(err).WithField("key", key).WithField("lang", lang).Debug("i18n key missing")
		return key
	}
	return msg
}

// THTML translates a message and returns template.HTML (unescaped).
// Use for translations containing trusted HTML (links, <strong>, etc.).
// Template usage: {{ tHTML $.Lang "instructions.stremio.heading" }}
func (h *Helper) THTML(lang string, key string) template.HTML {
	return template.HTML(h.T(lang, key))
}

// Tp translates a message with template data.
// Template usage: {{ tp $.Lang "footer.copyright" "Year" 2025 }}
func (h *Helper) Tp(lang string, key string, args ...any) string {
	loc := h.svc.Localizer(lang)
	data := make(map[string]any, len(args)/2)
	for i := 0; i+1 < len(args); i += 2 {
		data[fmt.Sprintf("%v", args[i])] = args[i+1]
	}
	msg, err := loc.Localize(&i18n.LocalizeConfig{
		MessageID:    key,
		TemplateData: data,
	})
	if err != nil {
		log.WithError(err).WithField("key", key).WithField("lang", lang).Debug("i18n key missing")
		return key
	}
	return msg
}

// TpHTML translates a message with template data and returns template.HTML (unescaped).
// Use for translations containing trusted HTML with dynamic parameters.
// Template usage: {{ tpHTML $.Lang "instructions.stremio.step1" "ProfileURL" (langPath $.Lang "/profile") }}
func (h *Helper) TpHTML(lang string, key string, args ...any) template.HTML {
	return template.HTML(h.Tp(lang, key, args...))
}

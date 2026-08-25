package i18n

import (
	"encoding/json"
	"fmt"
	"html/template"
	"regexp"
	"strings"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	log "github.com/sirupsen/logrus"
)

// resolve turns a (message, error) pair from go-i18n into the string to show.
//
// The subtlety it exists for: go-i18n v2 (v2.6.1, i18n/localizer.go
// getMessageTemplate) reports *MessageNotFoundErr whenever the requested
// language has no entry for the key — including the case where it went on to
// find the key in the bundle's default language and rendered that template
// successfully. LocalizeWithTag says so itself: "It may return a best effort
// localized message even if an error happens."
//
// So `err != nil` does not mean "nothing to show". Treating it that way is
// what turned a locale missing a key into a page full of raw identifiers
// (notifications.title, nav.notifications) when the English copy was sitting
// right there. Prefer whatever non-empty string came back; fall back to the
// key only when the key is genuinely absent from every locale, in which case
// go-i18n returns an empty message alongside the error.
func resolve(msg string, err error, key string, lang string) string {
	if msg != "" {
		if err != nil {
			log.WithError(err).WithField("key", key).WithField("lang", lang).
				Debug("i18n key missing in locale, fell back to default language")
		}
		return msg
	}
	if err != nil {
		log.WithError(err).WithField("key", key).WithField("lang", lang).
			Debug("i18n key missing")
	}
	return key
}

// TranslateWithLocalizer translates a key using the given Localizer.
// Falls back to the default language when the requested locale lacks the key,
// and returns the key itself only when no locale has it at all.
func TranslateWithLocalizer(loc *i18n.Localizer, key string) string {
	if loc == nil {
		return key
	}
	msg, err := loc.Localize(&i18n.LocalizeConfig{MessageID: key})
	return resolve(msg, err, key, "")
}

// TranslateWithLocalizerData translates a parameterized key using the given
// Localizer and template data. Use for messages like `{{.Bytes}}` / `{{.Peers}}`.
// Falls back to the default language when the requested locale lacks the key,
// and returns the key itself only when no locale has it at all.
func TranslateWithLocalizerData(loc *i18n.Localizer, key string, data map[string]any) string {
	if loc == nil {
		return key
	}
	msg, err := loc.Localize(&i18n.LocalizeConfig{
		MessageID:    key,
		TemplateData: data,
	})
	return resolve(msg, err, key, "")
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
// A locale missing the key degrades to the default-language (English) text,
// not to the raw key — see resolve.
// Template usage: {{ t $.Lang "nav.discover" }}
func (h *Helper) T(lang string, key string) string {
	loc := h.svc.Localizer(lang)
	msg, err := loc.Localize(&i18n.LocalizeConfig{MessageID: key})
	return resolve(msg, err, key, lang)
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
	return resolve(msg, err, key, lang)
}

// TpHTML translates a message with template data and returns template.HTML (unescaped).
// Use for translations containing trusted HTML with dynamic parameters.
// Template usage: {{ tpHTML $.Lang "instructions.stremio.step1" "ProfileURL" (langPath $.Lang "/profile") }}
func (h *Helper) TpHTML(lang string, key string, args ...any) template.HTML {
	return template.HTML(h.Tp(lang, key, args...))
}

// faqStripTagsRe removes any HTML tags from translated FAQ answer text
// before it goes into the schema.org/FAQPage JSON-LD payload. Google
// expects Answer.text to be plain text — leaving <strong>…</strong>
// in there does not break parsing but shows the literal tags in rich
// results. Our FAQ answers occasionally embed <strong> via tHTML.
var faqStripTagsRe = regexp.MustCompile(`<[^>]+>`)

// FaqSchema renders a schema.org/FAQPage JSON-LD block from the given
// (question_key, answer_key) pairs. Each argument is a single string
// in the form "qkey|akey" so templates can pass an arbitrary number
// of pairs without nested map literals. Use `|` (not `:`) because some
// answer keys carry colons in their values and using `|` keeps the
// separator outside the i18n key namespace as well.
//
// Template usage:
//
//	{{ faqSchema $.Lang
//	  "about.faq.free.q|about.faq.free.a"
//	  "about.faq.vpn.q|about.faq.vpn.a" }}
//
// Output is a ready-to-embed <script type="application/ld+json"> tag.
// Google ignores blocks where Question.name is empty, so missing
// translations degrade gracefully rather than emitting broken schema.
func (h *Helper) FaqSchema(lang string, pairs ...string) template.HTML {
	type answer struct {
		Type string `json:"@type"`
		Text string `json:"text"`
	}
	type question struct {
		Type           string `json:"@type"`
		Name           string `json:"name"`
		AcceptedAnswer answer `json:"acceptedAnswer"`
	}
	type faqPage struct {
		Context    string     `json:"@context"`
		Type       string     `json:"@type"`
		MainEntity []question `json:"mainEntity"`
	}

	items := make([]question, 0, len(pairs))
	for _, p := range pairs {
		parts := strings.SplitN(p, "|", 2)
		if len(parts) != 2 {
			log.WithField("pair", p).Warn("faqSchema: malformed pair, want \"qkey|akey\"")
			continue
		}
		q := strings.TrimSpace(h.T(lang, parts[0]))
		a := strings.TrimSpace(h.T(lang, parts[1]))
		if q == "" || q == parts[0] || a == "" || a == parts[1] {
			// Missing translation — i18n.Helper.T returns the key itself
			// in that case. Skip the entry rather than emit a JSON-LD
			// item that points at a non-existent question.
			continue
		}
		a = faqStripTagsRe.ReplaceAllString(a, "")
		items = append(items, question{
			Type: "Question",
			Name: q,
			AcceptedAnswer: answer{
				Type: "Answer",
				Text: a,
			},
		})
	}
	if len(items) == 0 {
		return ""
	}
	payload, err := json.Marshal(faqPage{
		Context:    "https://schema.org",
		Type:       "FAQPage",
		MainEntity: items,
	})
	if err != nil {
		log.WithError(err).Error("faqSchema: marshal failed")
		return ""
	}
	return template.HTML(`<script type="application/ld+json">` + string(payload) + `</script>`)
}

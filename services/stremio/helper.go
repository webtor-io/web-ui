package stremio

import "github.com/webtor-io/web-ui/models"

// Helper exposes Stremio-related data to Go HTML templates. Wired up via
// template.Manager.WithHelper, alongside web.Helper, i18n.Helper, etc.
type Helper struct{}

func NewHelper() *Helper {
	return &Helper{}
}

// StremioResolutions lists the resolution vocabulary the profile speaks —
// the same buckets PreferredStream sorts into and the subscription poller
// filters by, in the order the settings page shows them.
//
// Template usage: {{ range stremioResolutions }} ... {{ end }}.
func (s *Helper) StremioResolutions() []string {
	defaults := models.GetDefaultStremioSettings()
	out := make([]string, 0, len(defaults.PreferredResolutions))
	for _, r := range defaults.PreferredResolutions {
		out = append(out, r.Resolution)
	}
	return out
}

// StremioLanguageName resolves a language code to its display name, or ""
// when the code is unknown. Templates need it to render a stored preference
// ("ru") as something readable ("Russian") without carrying the whole list.
//
// Template usage: {{ stremioLanguageName "ru" }}.
func (s *Helper) StremioLanguageName(code string) string {
	if l := LanguageByCode(code); l != nil {
		return l.Name
	}
	return ""
}

// StremioLanguages returns the canonical, ordered list of supported
// languages for the Stremio addon's "preferred language" dropdown.
// Template usage: {{ range stremioLanguages }} ... {{ end }}.
func (s *Helper) StremioLanguages() []Language {
	return Languages
}

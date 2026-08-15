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

// StremioLanguages returns the canonical, ordered list of supported
// languages for the Stremio addon's "preferred language" dropdown.
// Template usage: {{ range stremioLanguages }} ... {{ end }}.
func (s *Helper) StremioLanguages() []Language {
	return Languages
}

package stremio

// Helper exposes Stremio-related data to Go HTML templates. Wired up via
// template.Manager.WithHelper, alongside web.Helper, i18n.Helper, etc.
type Helper struct{}

func NewHelper() *Helper {
	return &Helper{}
}

// StremioLanguages returns the canonical, ordered list of supported
// languages for the Stremio addon's "preferred language" dropdown.
// Template usage: {{ range stremioLanguages }} ... {{ end }}.
func (s *Helper) StremioLanguages() []Language {
	return Languages
}

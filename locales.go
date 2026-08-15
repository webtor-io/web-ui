package main

import (
	"embed"
	"io/fs"

	si18n "github.com/webtor-io/web-ui/services/i18n"
)

//go:embed locales/*.json
var localeFS embed.FS

// newI18n builds the translation bundle from the embedded locale files.
// The server is not its only consumer: notifications are rendered in the
// language of the account, and they are sent from cron commands that build
// no template manager and no gin engine.
func newI18n() *si18n.Service {
	locales, _ := fs.Sub(localeFS, "locales")
	return si18n.New(locales)
}

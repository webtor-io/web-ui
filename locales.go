package main

import "embed"

//go:embed locales/*.json
var localeFS embed.FS

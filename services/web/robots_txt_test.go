package web

import (
	"os"
	"strings"
	"testing"
)

// Google renders pages with the CSS and JS it is allowed to fetch. With
// /assets/ disallowed it indexed the unstyled HTML for six months
// (2026-03-29 .. 2026-09-18): the URL Inspection screenshot showed bare
// bullet lists and full-width SVG icons instead of the hero.
func TestRobotsTxtLetsCrawlersFetchAssets(t *testing.T) {
	b, err := os.ReadFile("../../pub/robots.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		l := strings.ToLower(strings.TrimSpace(line))
		if strings.HasPrefix(l, "disallow:") && strings.Contains(l, "/assets") {
			t.Errorf("robots.txt blocks render resources: %q", line)
		}
	}
}

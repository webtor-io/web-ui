package scripts

import (
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/web"
)

func isPaidForTranslate(c *web.Context) bool {
	return c != nil && c.Claims != nil && c.Claims.Context != nil && c.Claims.Context.Tier != nil && c.Claims.Context.Tier.Id != 0
}

// buildSubtitleOpts assembles the viewer-facing subtitle-ladder inputs.
// adult switches the AI track off entirely for NSFW resources (spec decision 13):
// no item, no lock, no CTA — the ladder simply never sees Translate=true.
func buildSubtitleOpts(c *web.Context, enabled, freeForAll, adult bool, preferred string, names []string) models.SubtitleOpts {
	return models.SubtitleOpts{
		PreferredLang: preferred,
		Translate:     enabled && !adult,
		Paid:          freeForAll || isPaidForTranslate(c),
		Names:         names,
	}
}

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
// embed does the same for the embed widget, which does not get AI
// translation in phase 2: the CTA has nowhere to lead on a third-party
// page, and the cost would be charged to a viewer we cannot identify.
func buildSubtitleOpts(c *web.Context, enabled, freeForAll, adult, embed bool, preferred string, names []string) models.SubtitleOpts {
	return models.SubtitleOpts{
		PreferredLang: preferred,
		Translate:     enabled && !adult && !embed,
		Paid:          freeForAll || isPaidForTranslate(c),
		Names:         names,
	}
}

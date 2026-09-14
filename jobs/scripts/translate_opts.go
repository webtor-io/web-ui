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

// subtitleOptsFor is the master switch in front of buildSubtitleOpts. The
// feature flag and the embed widget do not merely withhold the AI item:
// they put the page back on the phase-1 selection entirely. That is what an
// empty PreferredLang means to GetSubtitles -- it takes the legacy
// selectListItem path and never enters applyLadder -- so a deployment that
// never switched the feature on keeps the exact behaviour it had, forced
// tracks and audio rule included, and an embed keeps the selection its
// third-party host has been getting all along.
//
// buildSubtitleOpts keeps its own adult/embed gates: this is the switch,
// those are defence in depth for any other caller.
// DebugTierFree is the debug value that previews the stream page as a
// free viewer: the AI track renders locked with the CTA even for a paid
// account. It only ever downgrades, so the action handler lets it
// through in release builds too (the other debug values stay dev-only).
const DebugTierFree = "tier:free"

// previewAsFree applies DebugTierFree to the computed options.
func previewAsFree(o models.SubtitleOpts, debug string) models.SubtitleOpts {
	if debug == DebugTierFree {
		o.Paid = false
	}
	return o
}

func subtitleOptsFor(enabled, embed bool, c *web.Context, freeForAll, adult bool, preferred string, names []string) models.SubtitleOpts {
	if !enabled || embed {
		return models.SubtitleOpts{}
	}
	return buildSubtitleOpts(c, enabled, freeForAll, adult, embed, preferred, names)
}

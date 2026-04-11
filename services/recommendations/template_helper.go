package recommendations

import (
	"strings"

	"github.com/urfave/cli"
)

// Helper exposes recommendation config to Go templates. Registered with
// the TemplateManager in serve.go via WithHelper, so its public methods
// become template functions named with the first letter lower-cased
// (Helper.AiFreeQuota → {{ aiFreeQuota }}).
//
// All methods are read-only and side-effect free; safe to call from any
// template render. The helper holds a snapshot of the config taken at
// startup — restart web-ui to pick up env var changes.
type Helper struct {
	cfg Config
}

// NewHelper builds a Helper from the urfave/cli context. Mirrors the
// shape of umami.NewHelper / geoip.NewHelper used elsewhere in serve.go.
func NewHelper(c *cli.Context) *Helper {
	return &Helper{cfg: ConfigFromCLI(c)}
}

// AiEnabled mirrors the conditions under which rec.New returns a
// non-nil Service: feature flag on AND ANTHROPIC_API_KEY present.
// Templates use this to hide AI-related copy entirely when the feature
// is disabled, so we don't advertise something the user can't try.
func (h *Helper) AiEnabled() bool {
	return h.cfg.Enabled && strings.TrimSpace(h.cfg.AnthropicAPIKey) != ""
}

// AiFreeQuota returns the per-day request budget for free-tier users.
// Used in the about-page FAQ to keep the displayed limit honest when
// AI_RECOMMENDATIONS_FREE_DAILY_QUOTA changes via env.
func (h *Helper) AiFreeQuota() int {
	return h.cfg.FreeDailyQuota
}

// AiPaidQuota returns the per-day request budget for paid-tier users.
func (h *Helper) AiPaidQuota() int {
	return h.cfg.PaidDailyQuota
}

package recommendations

import (
	"github.com/urfave/cli"
)

// CLI flag names. Kept as exported constants so that tests and handlers can
// look them up via `c.Bool(FlagEnabled)` etc without stringly-typed typos.
const (
	FlagEnabled         = "ai-recommendations-enabled"
	FlagAnthropicAPIKey = "anthropic-api-key"
	// FlagModel is the legacy single-model knob. If set, both tiers use it
	// unless overridden by the tier-specific flags below. Kept for backwards
	// compatibility with the original Config.
	FlagModel          = "ai-recommendations-model"
	FlagFreeModel      = "ai-recommendations-free-model"
	FlagPaidModel      = "ai-recommendations-paid-model"
	FlagChipsModel     = "ai-recommendations-chips-model"
	FlagFreeDailyQuota = "ai-recommendations-free-daily-quota"
	FlagPaidDailyQuota = "ai-recommendations-paid-daily-quota"
	FlagMaxQueryLength = "ai-recommendations-max-query-length"
	FlagHistoryLimit   = "ai-recommendations-history-limit"
	FlagChipsTTL              = "ai-recommendations-chips-ttl-seconds"
	FlagRecsTTL               = "ai-recommendations-recs-ttl-seconds"
	FlagFreshReleasesMinYear  = "ai-recommendations-fresh-releases-min-year"
	FlagFreshReleasesLimit    = "ai-recommendations-fresh-releases-limit"
	FlagFreshReleasesCacheTTL = "ai-recommendations-fresh-releases-cache-ttl-seconds"
)

// DefaultModel is the Claude model we target out of the box. Haiku 4.5 has
// the best cost/latency trade-off for a constrained recommendation task.
const DefaultModel = "claude-haiku-4-5-20251001"

// Config is the resolved configuration for the recommendations service.
// Populated from CLI flags / env vars at wiring time.
type Config struct {
	Enabled         bool
	AnthropicAPIKey string
	// Model is the legacy single-model setting. When set, it acts as the
	// fallback for both tiers if neither FreeModel nor PaidModel is given.
	// New deployments should prefer FreeModel + PaidModel.
	Model           string
	FreeModel       string
	PaidModel       string
	ChipsModel      string
	FreeDailyQuota  int
	PaidDailyQuota  int
	MaxQueryLength  int
	HistoryLimit    int
	ChipsTTLSeconds        int
	RecsTTLSeconds         int
	FreshReleasesMinYear   int
	FreshReleasesLimit     int
	FreshReleasesCacheTTL  int
}

// RegisterFlags registers all CLI flags for the recommendations service.
// Invoked from serve.go via configureServe.
func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.BoolFlag{
			Name:   FlagEnabled,
			Usage:  "enable AI recommendations feature in Discover",
			EnvVar: "AI_RECOMMENDATIONS_ENABLED",
		},
		cli.StringFlag{
			Name:   FlagAnthropicAPIKey,
			Usage:  "Anthropic API key (required when AI recommendations are enabled)",
			EnvVar: "ANTHROPIC_API_KEY",
		},
		cli.StringFlag{
			Name:   FlagModel,
			Usage:  "Claude model id (legacy — used as fallback for both tiers when free/paid overrides are unset)",
			Value:  DefaultModel,
			EnvVar: "AI_RECOMMENDATIONS_MODEL",
		},
		cli.StringFlag{
			Name:   FlagFreeModel,
			Usage:  "Claude model id for free-tier users (overrides --ai-recommendations-model)",
			EnvVar: "AI_RECOMMENDATIONS_FREE_MODEL",
		},
		cli.StringFlag{
			Name:   FlagPaidModel,
			Usage:  "Claude model id for paid-tier users (overrides --ai-recommendations-model)",
			EnvVar: "AI_RECOMMENDATIONS_PAID_MODEL",
		},
		cli.StringFlag{
			Name:   FlagChipsModel,
			Usage:  "Claude model id for chip generation (defaults to free-tier model if unset — chips are lightweight and don't benefit from a smarter model)",
			EnvVar: "AI_RECOMMENDATIONS_CHIPS_MODEL",
		},
		cli.IntFlag{
			Name:   FlagFreeDailyQuota,
			Usage:  "daily AI recommendation quota for free tier users",
			Value:  1,
			EnvVar: "AI_RECOMMENDATIONS_FREE_DAILY_QUOTA",
		},
		cli.IntFlag{
			Name:   FlagPaidDailyQuota,
			Usage:  "daily AI recommendation quota for paid tier users (anti-abuse cap)",
			Value:  100,
			EnvVar: "AI_RECOMMENDATIONS_PAID_DAILY_QUOTA",
		},
		cli.IntFlag{
			Name:   FlagMaxQueryLength,
			Usage:  "maximum length of a user-provided recommendation query in characters",
			Value:  500,
			EnvVar: "AI_RECOMMENDATIONS_MAX_QUERY_LENGTH",
		},
		cli.IntFlag{
			Name:   FlagHistoryLimit,
			Usage:  "how many recent watched/rated movies to feed the prompt",
			Value:  40,
			EnvVar: "AI_RECOMMENDATIONS_HISTORY_LIMIT",
		},
		cli.IntFlag{
			Name:   FlagChipsTTL,
			Usage:  "how long generated chips are cached per user (seconds)",
			Value:  4 * 60 * 60,
			EnvVar: "AI_RECOMMENDATIONS_CHIPS_TTL_SECONDS",
		},
		cli.IntFlag{
			Name:   FlagRecsTTL,
			Usage:  "how long generated recommendations are cached per (user, query) (seconds)",
			Value:  30 * 60,
			EnvVar: "AI_RECOMMENDATIONS_RECS_TTL_SECONDS",
		},
		cli.IntFlag{
			Name:   FlagFreshReleasesMinYear,
			Usage:  "minimum release year for fresh releases injected into the AI prompt (compensates for Claude's training cutoff)",
			Value:  2025,
			EnvVar: "AI_RECOMMENDATIONS_FRESH_RELEASES_MIN_YEAR",
		},
		cli.IntFlag{
			Name:   FlagFreshReleasesLimit,
			Usage:  "max number of recent films to load into the AI prompt",
			Value:  200,
			EnvVar: "AI_RECOMMENDATIONS_FRESH_RELEASES_LIMIT",
		},
		cli.IntFlag{
			Name:   FlagFreshReleasesCacheTTL,
			Usage:  "how long the in-memory fresh releases prompt block is cached (seconds)",
			Value:  3600,
			EnvVar: "AI_RECOMMENDATIONS_FRESH_RELEASES_CACHE_TTL_SECONDS",
		},
	)
}

// ConfigFromCLI reads a Config struct from the urfave/cli context.
func ConfigFromCLI(c *cli.Context) Config {
	return Config{
		Enabled:         c.Bool(FlagEnabled),
		AnthropicAPIKey: c.String(FlagAnthropicAPIKey),
		Model:           c.String(FlagModel),
		FreeModel:       c.String(FlagFreeModel),
		PaidModel:       c.String(FlagPaidModel),
		ChipsModel:      c.String(FlagChipsModel),
		FreeDailyQuota:  c.Int(FlagFreeDailyQuota),
		PaidDailyQuota:  c.Int(FlagPaidDailyQuota),
		MaxQueryLength:  c.Int(FlagMaxQueryLength),
		HistoryLimit:    c.Int(FlagHistoryLimit),
		ChipsTTLSeconds:       c.Int(FlagChipsTTL),
		RecsTTLSeconds:        c.Int(FlagRecsTTL),
		FreshReleasesMinYear:  c.Int(FlagFreshReleasesMinYear),
		FreshReleasesLimit:    c.Int(FlagFreshReleasesLimit),
		FreshReleasesCacheTTL: c.Int(FlagFreshReleasesCacheTTL),
	}
}

// ResolveChipsModel returns the Claude model id used for chip generation.
// Override chain: ChipsModel → free-tier model → legacy Model → DefaultModel.
// Chips are lightweight (6 short labels) so the default intentionally
// falls back to the free-tier model (Haiku), not the paid one.
func (c Config) ResolveChipsModel() string {
	if c.ChipsModel != "" {
		return c.ChipsModel
	}
	return c.ResolveModel(TierFree)
}

// ResolveModel returns the effective Claude model id for the given tier,
// applying the override chain: tier-specific → legacy Model → DefaultModel.
func (c Config) ResolveModel(tier Tier) string {
	specific := c.FreeModel
	if tier == TierPaid {
		specific = c.PaidModel
	}
	if specific != "" {
		return specific
	}
	if c.Model != "" {
		return c.Model
	}
	return DefaultModel
}

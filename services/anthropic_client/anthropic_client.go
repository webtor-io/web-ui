// Package anthropic_client owns the single shared *anthropic.Client used by
// every web-ui feature that talks to Claude (AI Discover recommendations and
// AI enrichment so far). Pulling the client out of any one consumer keeps
// the API-key flag, the prompt-caching beta header, and any future
// transport-level concerns (timeouts, retries, custom HTTP) in one place.
package anthropic_client

import (
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

// FlagAnthropicAPIKey is the single shared CLI flag for the Anthropic API
// key. Multiple consumers read it via c.String(FlagAnthropicAPIKey); the
// flag itself is registered exactly once via RegisterFlags.
const FlagAnthropicAPIKey = "anthropic-api-key"

// RegisterFlags adds the API-key flag to the given slice. Must be called
// once per CLI command that needs Claude access. Calling it twice on the
// same command produces a duplicate-flag panic from urfave/cli.
func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:   FlagAnthropicAPIKey,
			Usage:  "Anthropic API key (shared by AI recommendations and AI enrichment)",
			EnvVar: "ANTHROPIC_API_KEY",
		},
	)
}

// New returns a configured *anthropic.Client, or nil when the API key is
// missing or blank. Returning nil (not an error) lets every caller treat
// "no key configured" as "feature disabled" without bespoke nil-key
// handling at each site.
//
// The prompt-caching-2024-07-31 beta header is always set so that
// cache_control blocks on system prompts work across every Claude family.
// The header is harmless on models that have caching GA — they just
// ignore it.
func New(c *cli.Context) *anthropic.Client {
	key := strings.TrimSpace(c.String(FlagAnthropicAPIKey))
	if key == "" {
		log.Info("anthropic_client: ANTHROPIC_API_KEY empty — Claude features disabled")
		return nil
	}
	cl := anthropic.NewClient(
		option.WithAPIKey(key),
		option.WithHeader("anthropic-beta", "prompt-caching-2024-07-31"),
	)
	return &cl
}

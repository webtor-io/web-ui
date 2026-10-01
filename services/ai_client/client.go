// Package ai_client shares AI provider selection and SDK adapters across
// Discover recommendations and metadata enrichment.
package ai_client

import (
	"context"
	"encoding/json"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

const (
	FlagAnthropicAPIKey = "anthropic-api-key"
	FlagOpenAIAPIKey    = "openai-api-key"
	FlagOpenAIBaseURL   = "openai-base-url"
)

type Provider string

const (
	Anthropic             Provider = "anthropic"
	OpenAI                Provider = "openai"
	DefaultAnthropicModel          = "claude-haiku-4-5-20251001"
	DefaultOpenAIModel             = "gpt-4.1-mini"
)

// SelectProvider depends only on configured credentials, never on reachability.
// Anthropic takes precedence when both keys are present to preserve existing
// deployments. API errors do not switch providers.
func SelectProvider(anthropicKey, openaiKey string) Provider {
	if strings.TrimSpace(anthropicKey) != "" {
		return Anthropic
	}
	if strings.TrimSpace(openaiKey) != "" {
		return OpenAI
	}
	return ""
}

func ProviderFromCLI(c *cli.Context) Provider {
	return SelectProvider(c.String(FlagAnthropicAPIKey), c.String(FlagOpenAIAPIKey))
}

func DefaultModel(provider Provider) string {
	if provider == OpenAI {
		return DefaultOpenAIModel
	}
	return DefaultAnthropicModel
}

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{Name: FlagAnthropicAPIKey, Usage: "Anthropic API key (shared by AI recommendations and enrichment; takes precedence over OpenAI)", EnvVar: "ANTHROPIC_API_KEY"},
		cli.StringFlag{Name: FlagOpenAIAPIKey, Usage: "OpenAI API key (shared by AI recommendations and enrichment)", EnvVar: "OPENAI_API_KEY"},
		cli.StringFlag{Name: FlagOpenAIBaseURL, Usage: "OpenAI API base URL", EnvVar: "OPENAI_BASE_URL", Value: "https://api.openai.com/v1"},
	)
}

func New(c *cli.Context) Client {
	provider := ProviderFromCLI(c)
	log.WithField("provider", provider).Info("ai_client: selected configured provider")
	switch provider {
	case Anthropic:
		if strings.TrimSpace(c.String(FlagOpenAIAPIKey)) != "" {
			log.Info("ai_client: both API keys configured — using Anthropic")
		}
		return newAnthropicClient(strings.TrimSpace(c.String(FlagAnthropicAPIKey)))
	case OpenAI:
		return newOpenAIClient(strings.TrimSpace(c.String(FlagOpenAIAPIKey)), c.String(FlagOpenAIBaseURL))
	default:
		log.Info("ai_client: no API key configured — AI features disabled")
		return nil
	}
}

type SystemBlock struct {
	Text  string
	Cache bool // Anthropic cache breakpoint; OpenAI caches prefixes automatically.
}

type Message struct {
	Role    string
	Content string
}

type Request struct {
	Model       string
	MaxTokens   int64
	System      []SystemBlock
	Messages    []Message
	Temperature float64 // Applied by Anthropic; OpenAI uses model defaults.
}

type Tool struct {
	Name        string
	Description string
	Properties  map[string]any
	Required    []string
}

type Usage struct {
	Model             string
	InputTokens       int64
	OutputTokens      int64
	CacheReadTokens   int64
	CacheCreateTokens int64
	StopReason        string
}

type ToolResponse struct {
	Input json.RawMessage
	Usage Usage
}

type Client interface {
	Provider() Provider
	CallTool(context.Context, Request, Tool) (ToolResponse, error)
	// StreamText calls onText synchronously for each delta and drains the stream
	// to its terminal event so final token and cache usage is retained.
	StreamText(ctx context.Context, req Request, onText func(string, Usage)) (Usage, error)
}

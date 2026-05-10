package enrich

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
)

// CLI flag names. Private — callers go through RegisterFlags + New, like
// every other service in this repo (see services/tmdb/api.go,
// services/kinopoisk_unofficial/api.go).
const (
	aiResolveEnabledFlag    = "ai-enrich-enabled"
	aiResolveModelFlag      = "ai-enrich-model"
	aiResolveTimeoutSecFlag = "ai-enrich-timeout-seconds"
	aiResolveMaxCandidates  = "ai-enrich-max-candidates"
)

// defaultAIResolveModel is what we ship out of the box. Haiku 4.5 is the
// right cost/latency point for a "normalize this release name" task —
// pattern recognition over Latin-transliterated foreign titles, not a
// knowledge-cutoff-bound IMDB lookup.
const defaultAIResolveModel = "claude-haiku-4-5-20251001"

const (
	aiResolveToolName = "return_candidates"
	// Maximum prompt input length per filename. Filenames over this are a
	// pathological release-name and not worth a token. 1024 bytes covers
	// even verbose multi-tag scene names.
	aiResolveMaxFilenameLen = 1024
	// Output stays small (3 candidates × ~30 tokens). 384 leaves headroom.
	aiResolveMaxTokens = 384
)

// RegisterFlags adds the AI-enrichment CLI flags to the given slice. The
// shared anthropic-api-key flag is registered separately by
// services/anthropic_client and is reused here.
func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.BoolFlag{
			Name:   aiResolveEnabledFlag,
			Usage:  "enable AI fallback for torrent metadata enrichment when title-search providers (TMDB/OMDB/KPU) all miss",
			EnvVar: "AI_ENRICH_ENABLED",
		},
		cli.StringFlag{
			Name:   aiResolveModelFlag,
			Usage:  "Claude model id used by the AI enrichment fallback",
			Value:  defaultAIResolveModel,
			EnvVar: "AI_ENRICH_MODEL",
		},
		cli.IntFlag{
			Name:   aiResolveTimeoutSecFlag,
			Usage:  "timeout (seconds) for a single Claude AI-enrichment call",
			Value:  30,
			EnvVar: "AI_ENRICH_TIMEOUT_SECONDS",
		},
		cli.IntFlag{
			Name:   aiResolveMaxCandidates,
			Usage:  "maximum number of (title, year) candidates Claude is asked to suggest per torrent",
			Value:  3,
			EnvVar: "AI_ENRICH_MAX_CANDIDATES",
		},
	)
}

// TitleCandidate is one of Claude's normalized name suggestions. Year is
// optional — Claude is encouraged to drop it when uncertain so the
// downstream TMDB/OMDB/KPU search runs without a year filter.
type TitleCandidate struct {
	Title    string
	Year     *int16
	Language string // ISO-639-1 hint, informational only
}

// AIResolver normalizes a parsed torrent title via Claude and returns
// candidate (title, year) tuples that the regular TMDB/OMDB/KPU mappers
// can search again. It does NOT try to identify an IMDB id directly:
// Claude has a knowledge cutoff and 2026+ releases would force it to
// guess. Pattern-recognising "Vot eto drama" as Russian transliteration
// of "Вот это драма" is on-the-other-hand cutoff-free.
//
// The actual film identity is resolved by re-running the existing
// metadata mappers against each candidate. Whatever the mappers return
// is what gets persisted — Claude never supplies poster URLs, plot, or
// IMDB ids, only suggestions for what to query.
//
// No bespoke cache: TMDB.Map / KPU.Map already cache (title, year) →
// tmdb_id in their own tables, so the second torrent with the same
// transliterated title triggers exactly one Haiku call (deterministic
// at temp=0) and the candidate-search piggy-backs on the existing
// caches. Adding our own cache would survive --force semantics in a
// way that breaks "user wants Claude to retry" — see
// docs/ai_enrichment.md.
type AIResolver struct {
	model         string
	maxCandidates int
	timeout       time.Duration
	client        *anthropic.Client
}

// New wires the resolver from CLI flags and a shared anthropic client.
// Returns nil (not an error) when the feature is disabled or the
// client/db are missing — Enricher treats a nil resolver as "skip the AI
// fallback entirely". The pg dependency is currently unused but kept on
// the constructor signature so a future per-resource cache or audit
// table can be added without touching call sites.
func New(c *cli.Context, client *anthropic.Client, _ *cs.PG) *AIResolver {
	if !c.Bool(aiResolveEnabledFlag) {
		log.Info("ai_enrich: feature flag off — AI fallback disabled")
		return nil
	}
	if client == nil {
		log.Warn("ai_enrich: enabled but anthropic client is nil (no API key) — AI fallback disabled")
		return nil
	}
	model := c.String(aiResolveModelFlag)
	if model == "" {
		model = defaultAIResolveModel
	}
	timeout := time.Duration(c.Int(aiResolveTimeoutSecFlag)) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	maxCandidates := c.Int(aiResolveMaxCandidates)
	if maxCandidates <= 0 {
		maxCandidates = 3
	}
	log.WithFields(log.Fields{
		"model":          model,
		"max_candidates": maxCandidates,
		"timeout":        timeout,
	}).Info("ai_enrich: AIResolver ready")
	return &AIResolver{
		model:         model,
		maxCandidates: maxCandidates,
		timeout:       timeout,
		client:        client,
	}
}

// SuggestCandidates asks Claude to normalize the parsed torrent title
// into 1-3 alternative (title, year) candidates that real metadata DBs
// would index this work under. Returns an empty slice when Claude has
// no useful suggestion (e.g. the parsed title is already canonical) or
// the API call fails (logged and swallowed — AI is best-effort).
//
// The caller is expected to feed each candidate back through the
// existing mapper chain (TMDB.Map, OMDB.Map, KPU.Map). Whatever the
// mappers return is the validated metadata; Claude's role is purely to
// generate alternative search keys.
func (r *AIResolver) SuggestCandidates(ctx context.Context, pathStr, parsedTitle string, parsedYear *int16, ct models.ContentType) []TitleCandidate {
	if pathStr == "" {
		return nil
	}
	candidates, err := r.callClaude(ctx, pathStr, parsedTitle, parsedYear, ct)
	if err != nil {
		log.WithError(err).WithField("path", pathStr).Warn("ai_enrich: claude call failed")
		return nil
	}
	candidates = dedupeCandidates(candidates, parsedTitle, parsedYear)
	if len(candidates) > r.maxCandidates {
		candidates = candidates[:r.maxCandidates]
	}
	if len(candidates) == 0 {
		log.WithField("path", pathStr).Info("ai_enrich: claude returned no usable candidates")
		return nil
	}
	for i, cand := range candidates {
		yr := ""
		if cand.Year != nil {
			yr = strconv.Itoa(int(*cand.Year))
		}
		log.WithFields(log.Fields{
			"path":     pathStr,
			"index":    i,
			"title":    cand.Title,
			"year":     yr,
			"language": cand.Language,
		}).Info("ai_enrich: claude candidate")
	}
	return candidates
}

// callClaude runs the tool-use call and returns the raw candidates.
func (r *AIResolver) callClaude(ctx context.Context, pathStr, parsedTitle string, parsedYear *int16, ct models.ContentType) ([]TitleCandidate, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tool := anthropic.ToolParam{
		Name:        aiResolveToolName,
		Description: anthropic.String("Return up to 3 alternative (title, year) tuples that real metadata DBs (TMDB, IMDB, Kinopoisk) would index this torrent's content under. Empty array means no useful normalization is possible."),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: map[string]any{
				"candidates": map[string]any{
					"type":     "array",
					"maxItems": 3,
					"items": map[string]any{
						"type":     "object",
						"required": []string{"title"},
						"properties": map[string]any{
							"title": map[string]any{
								"type":        "string",
								"maxLength":   200,
								"description": "Canonical title in its native script — e.g. 'Вот это драма', 'The Drama', '오징어 게임'. Do NOT include release tags, codec markers, or year.",
							},
							"year": map[string]any{
								"type":        []string{"integer", "null"},
								"description": "Best-guess release year. Use null when unsure — searching without a year filter is preferred over a wrong year.",
							},
							"language": map[string]any{
								"type":        "string",
								"maxLength":   3,
								"description": "ISO-639-1 language code of `title` (e.g. 'ru', 'en', 'ko'). Optional, informational.",
							},
						},
					},
				},
				"reasoning": map[string]any{
					"type":        "string",
					"maxLength":   200,
					"description": "One short sentence on what transformation you applied (transliteration → Cyrillic, expanded abbreviation, etc). Empty when no candidates.",
				},
			},
			Required: []string{"candidates"},
		},
	}

	userPrompt := r.buildUserPrompt(pathStr, parsedTitle, parsedYear, ct)
	resp, err := r.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(r.model),
		MaxTokens: aiResolveMaxTokens,
		System:    []anthropic.TextBlockParam{{Text: aiResolveSystemPrompt}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt)),
		},
		Tools:       []anthropic.ToolUnionParam{{OfTool: &tool}},
		ToolChoice:  anthropic.ToolChoiceParamOfTool(aiResolveToolName),
		Temperature: anthropic.Float(0.0),
	})
	if err != nil {
		return nil, errors.Wrap(err, "anthropic messages.new")
	}

	log.WithFields(log.Fields{
		"feature":       "ai_enrich",
		"model":         resp.Model,
		"input_tokens":  resp.Usage.InputTokens,
		"output_tokens": resp.Usage.OutputTokens,
		"stop_reason":   resp.StopReason,
	}).Info("ai_enrich: claude call complete")

	raw, err := extractAIResolveToolUse(resp.Content)
	if err != nil {
		return nil, err
	}

	var payload struct {
		Candidates []struct {
			Title    string `json:"title"`
			Year     *int   `json:"year"`
			Language string `json:"language"`
		} `json:"candidates"`
		Reasoning string `json:"reasoning"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, errors.Wrap(err, "ai_enrich: tool input json")
	}

	if payload.Reasoning != "" {
		log.WithField("reasoning", payload.Reasoning).Debug("ai_enrich: claude reasoning")
	}

	out := make([]TitleCandidate, 0, len(payload.Candidates))
	for _, c := range payload.Candidates {
		title := strings.TrimSpace(c.Title)
		if title == "" {
			continue
		}
		var yr *int16
		if c.Year != nil && *c.Year > 1888 && *c.Year < 2100 {
			y := int16(*c.Year)
			yr = &y
		}
		out = append(out, TitleCandidate{
			Title:    title,
			Year:     yr,
			Language: strings.TrimSpace(c.Language),
		})
	}
	return out, nil
}

func (r *AIResolver) buildUserPrompt(pathStr, parsedTitle string, parsedYear *int16, ct models.ContentType) string {
	if len(pathStr) > aiResolveMaxFilenameLen {
		pathStr = pathStr[:aiResolveMaxFilenameLen]
	}
	hint := "movie"
	if ct == models.ContentTypeSeries {
		hint = "series"
	}
	year := ""
	if parsedYear != nil {
		year = "\nParsed year: " + strconv.Itoa(int(*parsedYear))
	}
	return "Torrent path: " + pathStr +
		"\nParsed title: " + parsedTitle +
		year +
		"\nMedia type hint (from heuristic, may be wrong): " + hint
}

func extractAIResolveToolUse(blocks []anthropic.ContentBlockUnion) (json.RawMessage, error) {
	for _, b := range blocks {
		if b.Type == "tool_use" && b.Name == aiResolveToolName {
			if len(b.Input) == 0 {
				return nil, errors.Errorf("ai_enrich: empty tool input")
			}
			return json.RawMessage(b.Input), nil
		}
	}
	return nil, errors.Errorf("ai_enrich: tool %s not called (blocks=%d)", aiResolveToolName, len(blocks))
}

// dedupeCandidates removes duplicates and any candidate that exactly
// matches the parsed (title, year) the regular mappers already tried.
// Without this filter we'd burn API calls re-searching the same key.
func dedupeCandidates(in []TitleCandidate, parsedTitle string, parsedYear *int16) []TitleCandidate {
	parsedKey := normTitle(parsedTitle) + "|" + yearKey(parsedYear)
	seen := map[string]bool{}
	out := make([]TitleCandidate, 0, len(in))
	for _, c := range in {
		key := normTitle(c.Title) + "|" + yearKey(c.Year)
		if seen[key] {
			continue
		}
		seen[key] = true
		if key == parsedKey {
			continue
		}
		out = append(out, c)
	}
	return out
}

func normTitle(t string) string {
	return strings.ToLower(strings.TrimSpace(t))
}

func yearKey(y *int16) string {
	if y == nil {
		return ""
	}
	return strconv.Itoa(int(*y))
}

const aiResolveSystemPrompt = `You normalize torrent release names into clean (title, year) search keys for a metadata pipeline.

Input: a torrent path, a heuristic-parsed title, year, and media type. The pipeline already searched TMDB/OMDB/Kinopoisk with the parsed title and got nothing.

Your job is to suggest 1-3 alternative (title, year) tuples that THE SAME real metadata DBs (TMDB / IMDB / Kinopoisk) might index this work under. You do NOT need to know the film personally — you only need to recognize what transformation the parsed title underwent.

You are NOT trying to identify an IMDB id. The pipeline will run TMDB/OMDB/Kinopoisk search on each candidate you return; whatever they find IS the answer. So return search keys, not facts.

Common transformations to reverse:
- Latin transliteration of Cyrillic → original Cyrillic. "Vot.eto.drama" → "Вот это драма". "Bolshaya.dvadcatka" → "Большая двадцатка". "Mayor.Grom" → "Майор Гром". GOST/passport translit conventions cover most cases.
- Latin transliteration of Korean/Japanese/Chinese/Arabic → native script when distinguishable, else common English title.
- For non-English originals, ALSO suggest the canonical English title when a well-known one exists (TMDB indexes both — having two candidates increases hit rate). For unknown films, leave the English candidate out rather than guessing.
- Abbreviations: "Mr." vs "Mister", roman numerals vs arabic, etc. Provide both spellings if either could be the indexed form.
- Year correction: if the filename has a re-release year (e.g. an old film with a 2025 BluRay tag), prefer dropping the year (year=null) so search runs unconstrained.

Hard rules:
- Do NOT just echo the parsed title back — the pipeline already searched that and missed.
- Title field must contain ONLY the title — no codec tags, no year, no group names.
- If the parsed title is already canonical and you have no alternative to offer, return an empty candidates array.
- Quality over quantity: 1 well-targeted candidate beats 3 noisy ones. Cap is 3.

Examples:
  parsed "Vot eto drama" 2026 → [{"title": "Вот это драма", "year": 2026, "language": "ru"}, {"title": "The Drama", "year": 2026, "language": "en"}]
  parsed "Mayor Grom" 2021    → [{"title": "Майор Гром: Чумной Доктор", "year": 2021, "language": "ru"}, {"title": "Major Grom: Plague Doctor", "year": 2021, "language": "en"}]
  parsed "Sun San Hong Si"     → [{"title": "新三国", "year": null, "language": "zh"}, {"title": "Three Kingdoms", "year": null, "language": "en"}]
  parsed "The Matrix" 1999     → []     (already canonical, nothing to add)

Always call the return_candidates tool exactly once.`

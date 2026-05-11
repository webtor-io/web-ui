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
	aem "github.com/webtor-io/web-ui/models/ai_enrich"
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
// Outcomes are cached by (parsed_title, parsed_year, content_type) in
// ai_enrich.query so the second torrent with the same parsed title
// never re-calls Claude (his answer is deterministic at temp=0 anyway).
// Negative outcomes ("no useful normalization") are cached as empty
// arrays. `--force` bypasses the cache and re-queries Claude, matching
// the TMDB/KPU.Map cache semantics.
type AIResolver struct {
	model         string
	maxCandidates int
	timeout       time.Duration
	client        *anthropic.Client
	pg            *cs.PG
}

// New wires the resolver from CLI flags and a shared anthropic client.
// Returns nil (not an error) when the feature is disabled or the
// client/db are missing — Enricher treats a nil resolver as "skip the AI
// fallback entirely".
func New(c *cli.Context, client *anthropic.Client, pg *cs.PG) *AIResolver {
	if !c.Bool(aiResolveEnabledFlag) {
		log.Info("ai_enrich: feature flag off — AI fallback disabled")
		return nil
	}
	if client == nil {
		log.Warn("ai_enrich: enabled but anthropic client is nil (no API key) — AI fallback disabled")
		return nil
	}
	if pg == nil {
		log.Warn("ai_enrich: enabled but pg is nil — AI fallback disabled")
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
		pg:            pg,
	}
}

// SuggestCandidates asks Claude to normalize the parsed torrent title
// into 1-3 alternative (title, year) candidates that real metadata DBs
// would index this work under. Returns an empty slice when Claude has
// no useful suggestion (e.g. the parsed title is already canonical) or
// the API call fails (logged and swallowed — AI is best-effort).
//
// Cache lookup short-circuits Claude when this (parsed_title, year,
// content_type) tuple was seen before — negative entries return an
// empty slice without burning a Claude call. `force` bypasses the
// cache, matching the TMDB/KPU.Map convention.
//
// The caller is expected to feed each candidate back through the
// existing mapper chain (TMDB.Map, OMDB.Map, KPU.Map). Whatever the
// mappers return is the validated metadata; Claude's role is purely to
// generate alternative search keys.
func (r *AIResolver) SuggestCandidates(ctx context.Context, pathStr, parsedTitle string, parsedYear *int16, ct models.ContentType, force bool) []TitleCandidate {
	if pathStr == "" {
		return nil
	}
	ctInt := contentTypeToInt(ct)
	db := r.pg.Get()
	if db == nil {
		log.Warn("ai_enrich: db unavailable, skipping cache")
	} else if !force {
		if cached, err := aem.GetQuery(ctx, db, parsedTitle, parsedYear, ctInt); err != nil {
			log.WithError(err).Warn("ai_enrich: cache read failed, falling through to claude")
		} else if cached != nil {
			log.WithFields(log.Fields{
				"parsed_title": parsedTitle,
				"count":        len(cached.Candidates),
			}).Debug("ai_enrich: cache hit")
			return fromModelCandidates(cached.Candidates)
		}
	}

	candidates, err := r.callClaude(ctx, pathStr, parsedTitle, parsedYear, ct)
	if err != nil {
		log.WithError(err).WithField("path", pathStr).Warn("ai_enrich: claude call failed")
		// Do NOT cache transient API errors — next request may succeed.
		return nil
	}
	candidates = dedupeCandidates(candidates, parsedTitle, parsedYear)
	if len(candidates) > r.maxCandidates {
		candidates = candidates[:r.maxCandidates]
	}

	// Persist regardless of whether the answer was useful — empty array
	// is the negative cache and is what stops the next torrent with the
	// same parsed title from re-calling Claude.
	if db != nil {
		if err := aem.UpsertQuery(ctx, db, parsedTitle, parsedYear, ctInt, toModelCandidates(candidates), r.model); err != nil {
			log.WithError(err).Warn("ai_enrich: cache write failed")
		}
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

func toModelCandidates(in []TitleCandidate) []aem.Candidate {
	out := make([]aem.Candidate, len(in))
	for i, c := range in {
		out[i] = aem.Candidate{Title: c.Title, Year: c.Year, Language: c.Language}
	}
	return out
}

func fromModelCandidates(in []aem.Candidate) []TitleCandidate {
	if len(in) == 0 {
		return nil
	}
	out := make([]TitleCandidate, len(in))
	for i, c := range in {
		out[i] = TitleCandidate{Title: c.Title, Year: c.Year, Language: c.Language}
	}
	return out
}

func contentTypeToInt(ct models.ContentType) int16 {
	if ct == models.ContentTypeSeries {
		return 2
	}
	return 1
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
		// Prompt caching deliberately NOT enabled here. Empirical probing
		// against Haiku 4.5 (2026-05-11) shows the cache minimum is well
		// above this prompt's ~2.2k token system block — cache_control on
		// blocks under ~5k tokens is silently ignored (cache_creation_input_tokens=0).
		// Padding the prompt with filler just to cross the threshold would
		// hurt prompt quality without a real win: the ai_enrich.query
		// table already absorbs ~95% of duplicate parsed-titles, so the
		// remaining un-cached Claude calls are infrequent enough that
		// per-call savings don't justify a bloated prompt.
		System: []anthropic.TextBlockParam{{Text: aiResolveSystemPrompt}},
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

# What you are doing

Input: a torrent path (folder + filename), a heuristic-parsed title, a parsed year, and a media-type hint (movie or series). The enrichment pipeline already searched TMDB, OMDB, and Kinopoisk for the parsed title and got nothing. You are the last-resort normalizer.

Output: 1-3 alternative (title, year) tuples that those SAME real metadata DBs might index this work under. The pipeline will run TMDB/OMDB/Kinopoisk search on each candidate you return; whatever they find IS the answer. You generate search keys, not facts. You do NOT need to know the film personally — you only need to recognise what transformation the parsed title underwent.

You are NOT trying to identify an IMDB id. You are NOT trying to verify the film exists. You are NOT picking a winner. Just emit plausible search keys.

# How parsed titles get mangled (reverse these)

## Latin transliteration of Cyrillic → Cyrillic
This is the dominant case in our traffic. Russian, Ukrainian, Belarusian, Bulgarian, Serbian releases regularly transliterate the native title into Latin for the filename. GOST 7.79 (System B), ISO 9, and informal passport conventions are common; all are roughly reversible by pattern.

- "Vot.eto.drama" → "Вот это драма"
- "Bolshaya.dvadcatka" → "Большая двадцатка"
- "Mayor.Grom.Chumnoy.Doktor" → "Майор Гром: Чумной Доктор"
- "Bespridannitsa" → "Бесприданница"
- "Kholop" → "Холоп"
- "Sluga.Naroda" → "Слуга народа"
- "Brat" / "Brat-2" → "Брат" / "Брат-2"
- "Likvidatsiya" → "Ликвидация"
- "Beliy.tigr" → "Белый тигр"

Frequent transliteration cues: "kh"="х", "zh"="ж", "ts"="ц", "ch"="ч", "sh"="ш", "shch"="щ", "yu"/"ju"="ю", "ya"/"ja"="я", "yo"/"jo"="ё", "kh"/"h"="х", trailing "y" often is "ий"/"ый". Hyphenated word boundaries in the parsed title usually correspond to whitespace in the original.

## Latin transliteration of other scripts
Korean (Revised Romanisation / McCune-Reischauer), Japanese (Hepburn), Chinese (pinyin), Arabic (DIN 31635 / informal), Hindi (Hunterian), Persian (UN). When the script is identifiable, propose the native form AND the canonical English title TMDB is known to index. Examples:
- "Ojingeo geim" → "오징어 게임" + "Squid Game"
- "Shingeki no Kyojin" → "進撃の巨人" + "Attack on Titan"
- "Sangokushi" → "三国志" + "Three Kingdoms"
- "Ghazwat al-Quds" → "غزوة القدس"

If you cannot identify the source script with confidence, prefer just the well-known English title.

## English-only mangling
- Roman numerals vs Arabic: "Rocky.IV" and "Rocky 4" both index, prefer Arabic.
- Punctuation drop: "Spider-Man.Into.the.Spider-Verse" — keep the colon/hyphen as the DB indexes it.
- Subtitle drop: "Mission.Impossible.Fallout" → main title only or with subtitle, try whichever the parser ate.
- Abbreviated franchise: "MIB" → "Men in Black".
- Series sub-numbering noise: "S01E01" / "1x01" leaked into title — strip it.

## Year corrections
The parser sometimes catches a release/encode year instead of the production year (e.g. an old film with a "2025 BluRay" tag). If you suspect that, prefer year=null over a wrong number — TMDB search without a year filter is more forgiving than with a wrong one.

## Series-specific patterns
- Anime / drama releases often carry the season as a separate season number ("Shingeki.no.Kyojin.S04"). The parser strips "S04"; you propose the series title only, not "... Season 4".
- Long-running shows with year-ranges in the filename ("Doctor.Who.2005-2024") use the premiere year. Propose year=2005, not the range end.
- For shows whose English title was renamed mid-run (rare but real — e.g. Korean web-novels adapted twice), suggesting both the original-language title and the English title increases the chance that at least one DB indexes the right entry.
- "Specials" / "OVA" / "Movie" suffixes that the parser sometimes leaks into the title — strip them. Pipeline-side episode-mapping handles the season=0 case once the series is resolved.
- "(Dub)" / "(Sub)" / "[Eng Dub]" inside the title — strip. These are release variants, not separate works in TMDB.

## Tricky patterns that look mangled but aren't
- "X-Men", "Spider-Man", "WALL-E", "Wall·E" — punctuation IS part of the canonical title.
- "F1: The Movie" vs "F1" — keep the colon variant if it appears.
- Numbered franchise where Arabic vs Roman matters: "Star Wars: Episode IV" is TMDB-canonical with the Roman numeral; "Rocky 4" without subtitle is fine with Arabic.
- Foreign-language films whose ONLY indexed title is the English release name (Squid Game, Money Heist, Dark) — you can return just the English candidate, no native script needed.

# Hard rules

1. Do NOT echo the parsed title back. The pipeline already searched that exact string and missed. Repeating it wastes API quota.
2. Title field contains ONLY the title — no codec tags (WEB-DLRip, AVC, x264), no resolution markers (1080p), no audio tags (MVO, DUB, DD5.1), no release group names, no year, no quality (BluRay), no language tags (Rus, Eng).
3. Quality over quantity. One well-targeted candidate is better than three noisy ones. Cap is 3.
4. If the parsed title is already canonical and you have no alternative to offer, return an empty candidates array. Empty arrays are cached as negative results, which is correct: do not invent candidates to fill the slots.
5. Do not hallucinate film titles to match what you think the parser meant. If "Vot eto drama" is the input, "Вот это драма" is a SAFE proposal (it's a structural reverse-translit). "The Drama Without End" is NOT — that's invention. The risk of returning a confidently-wrong title outweighs the cost of returning nothing.
6. Language code is a 2-letter ISO 639-1 hint, optional. Skip if unsure.

# Worked examples

  parsed "Vot eto drama" year=2026 type=movie
    → [{"title": "Вот это драма", "year": 2026, "language": "ru"},
       {"title": "The Drama", "year": 2026, "language": "en"}]

  parsed "Mayor Grom" year=2021 type=movie
    → [{"title": "Майор Гром: Чумной Доктор", "year": 2021, "language": "ru"},
       {"title": "Major Grom: Plague Doctor", "year": 2021, "language": "en"}]

  parsed "Sun San Hong Si" year=null type=series
    → [{"title": "新三国", "year": null, "language": "zh"},
       {"title": "Three Kingdoms", "year": null, "language": "en"}]

  parsed "The Matrix" year=1999 type=movie
    → []

  parsed "Brat 2" year=2000 type=movie
    → [{"title": "Брат 2", "year": 2000, "language": "ru"}]

  parsed "Ojingeo geim" year=2021 type=series
    → [{"title": "오징어 게임", "year": 2021, "language": "ko"},
       {"title": "Squid Game", "year": 2021, "language": "en"}]

  parsed "asdfgh.random.garbage" year=null type=movie
    → []

Always call the return_candidates tool exactly once, even when the array is empty.`

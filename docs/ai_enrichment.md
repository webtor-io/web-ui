# AI enrichment fallback

When a torrent is added, `enrich.Enricher.Enrich` runs the parsed `(title, year)` through the metadata mappers in priority order — TMDB → OMDB → Kinopoisk Unofficial. For most releases at least one mapper hits and the pipeline persists posters, plot, rating, and IMDB id.

For some releases every mapper misses. The most common causes:

- Foreign-language films whose original title is transliterated to Latin in the filename (Russian `Vot.eto.drama.2026.iT.WEB-DLRip.AVC.mkv` → TMDB only indexes the Cyrillic `Вот это драма`, KPU only indexes the Cyrillic).
- Release-group quirks that confuse the title parser (extra dot/underscore patterns, codec/audio tags glued to the title).
- Films that exist in TMDB only under an obscure alternate title.

The AI enrichment fallback asks Claude to **normalize** the parsed title — translate Latin transliterations back to the native script, suggest the canonical English title, expand abbreviations — and re-runs the same TMDB/OMDB/KPU mappers against each candidate. Claude does **not** identify the IMDB id directly: that would force it to recall facts past its training cutoff (newer films wouldn't be known). Pattern-recognising "Vot eto drama" as Russian transliteration of "Вот это драма" is on-the-other-hand cutoff-free.

## Pipeline

```
mapMetadata(title, year, ct, pathHint)
  → TMDB.Map      (parsed title + year)           ── hit ─→ done
  → OMDB.Map      (parsed title + year)           ── hit ─→ done   ← errors logged + skipped
  → KPU.Map       (parsed title + year)           ── hit ─→ done
  → tryAIFallback(pathHint, parsed title, year, ct)
        → AIResolver.SuggestCandidates → []TitleCandidate{title, year, language}
        → for each candidate:
              for each mapper (TMDB, OMDB, KPU):
                  candVC = {Title: cand.Title, Year: cand.Year}
                  m.Map(candVC) ── hit ─→ tryUpgrade ─→ done
        → all missed → nil
```

The mapper loop is fault-tolerant: a single mapper returning an error (e.g. OMDB "Request limit reached!" on a free key) is logged and skipped instead of aborting the chain. Only when **every** path fails does the pipeline surface an error so `enrich --force-error` can retry later.

## Why candidates instead of an IMDB id

| Approach            | Pros                                | Cons                                                     |
|---------------------|-------------------------------------|----------------------------------------------------------|
| Claude → imdb_id    | One step                            | Knowledge cutoff: 2026+ films are unknown to Claude      |
| Claude → candidates | No cutoff; same recall as TMDB/etc. | One extra round of TMDB.Map per candidate (cached)        |

The pivot to candidates lifts the cutoff barrier entirely. Claude's job becomes "what real script is this transliteration of", which is a pattern-recognition task it can do regardless of whether the actual film is in its training set.

## No bespoke cache

Earlier iterations stored every AI outcome in `public.ai_enrich_resolution`. We dropped it because the table didn't carry its weight:

- **`TryInsertOrLockMediaInfo` already gates re-runs.** A `Status=Done/NoMedia/Error/Forbidden` resource is filtered out before `mapMetadata` is even called, so without `--force` Claude is never re-asked.
- **`--force` should re-ask Claude.** The whole point of force is "ignore caches and re-fetch". A negative-cache row that says "Claude said no last time" defeats this — a film that wasn't on TMDB last week might be there today, and the user invoking `--force` expects a fresh round-trip.
- **TMDB.query and KPU.query already cache the per-(title, year) miss/hit.** Once Claude's `"Вот это драма"` candidate hits TMDB and resolves to `tt33071426`, that result lands in `tmdb.query` automatically. The next torrent with the same parsed title pays one Haiku call (deterministic at temp=0) and zero TMDB API calls, because the candidate-search piggy-backs on the existing cache.
- **Race protection lives in `media_info`'s `FOR UPDATE SKIP LOCKED`.** Two parallel enrichers on the same hash never both reach AI.

What we'd lose in audit (which model resolved which torrent) is recoverable from logs.

## Configuration

All flags are off by default. Enabling requires `ANTHROPIC_API_KEY` (the same flag as AI recommendations).

| Flag                          | Env                          | Default                      | Meaning                                                  |
| ----------------------------- | ---------------------------- | ---------------------------- | -------------------------------------------------------- |
| `--ai-enrich-enabled`         | `AI_ENRICH_ENABLED`          | `false`                      | Master switch for the fallback                           |
| `--ai-enrich-model`           | `AI_ENRICH_MODEL`            | `claude-haiku-4-5-20251001`  | Claude model id                                           |
| `--ai-enrich-max-candidates`  | `AI_ENRICH_MAX_CANDIDATES`   | `3`                          | Cap on (title, year) suggestions per call                |
| `--ai-enrich-timeout-seconds` | `AI_ENRICH_TIMEOUT_SECONDS`  | `30`                         | Per-call timeout                                         |
| `--anthropic-api-key`         | `ANTHROPIC_API_KEY`          | (required)                   | Shared with AI recommendations                           |

The shared `*anthropic.Client` is constructed by `services/anthropic_client/`. AI recommendations and AI enrichment both consume the same client so the prompt-caching beta header lives in one place.

## Cost & latency

Latency is paid only on the miss path: ~95% of releases resolve via TMDB/OMDB/KPU and never touch Claude. Haiku 4.5 returns in roughly 0.5–2 s for this single-shot, low-output prompt. Each AI hit then triggers up to `max_candidates × len(mappers)` extra mapper search calls, but TMDB and KPU cache misses internally so the second torrent of the same parsed title re-uses those caches.

Token cost per call is dominated by the system prompt (~1.6k tokens, identical across calls — caches well) plus the filename (~30 tokens) and ~100 tokens of output.

## Worked example: `Vot.eto.drama.2026.iT.WEB-DLRip.AVC.mkv`

```
TMDB.Map("Vot eto drama", 2026)               → nil (no result for transliteration)
OMDB.Map(...)                                 → error "Request limit reached!"  (skipped)
KPU.Map("Vot eto drama", 2026)                → nil
AIResolver.SuggestCandidates(...)             → [
                                                  {title: "Вот это драма", year: 2026, lang: "ru"},
                                                  {title: "The Drama",     year: 2026, lang: "en"}
                                                ]
TMDB.Map("Вот это драма", 2026)               → tt33071426  ✓
movie_metadata: "The Drama" / 2026 / poster / plot / rating 7.0
```

## Files of interest

- `services/anthropic_client/anthropic_client.go` — shared SDK client constructor + API-key flag.
- `services/enrich/ai_resolver.go` — `AIResolver`, `RegisterFlags`, `New(c, client, _)`, `SuggestCandidates`, system prompt — all in one file (mirrors the pattern in `services/tmdb/api.go` and `services/kinopoisk_unofficial/api.go`).
- `services/enrich/enrich.go` — `mapMetadata` is fault-tolerant (per-mapper errors → log + skip) and calls `tryAIFallback` after the loop. `tryAIFallback` iterates candidates through the mapper chain.

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
        → isAdultPath(pathHint) ── true ─→ nil (no Claude, no cache write)
        → AIResolver.SuggestCandidates → []TitleCandidate{title, year, language}
        → for each candidate:
              for each mapper (TMDB, OMDB, KPU):
                  candVC = {Title: cand.Title, Year: cand.Year}
                  m.Map(candVC) ── hit ─→ tryUpgrade ─→ done
        → all missed → nil
```

For series, `pathHint` is the torrent's **root folder** (first non-empty segment of the representative episode path) rather than the per-episode filename. A pack named `Stand.Up.S13.Complete/01 - first joke.mkv` would otherwise hand Claude `01 - first joke` as both `parsed_title` (via the parser overwriting Title with the last path segment) and `pathHint`. Stripping back to `Stand.Up.S13.Complete` keeps the series-title signal alive even when individual episode filenames are bare numbered indexes.

## Adult-content prefilter

Adult releases (porn studio sites, JAV codes, explicit keywords, Chinese uncensored markers, Russian explicit verbs) are never enrichable through TMDB/OMDB/KPU and were filling the `ai_enrich.query` negative cache with ~30% pure waste (2026-05-11 telemetry: 693 of 2333 cache rows match by `parsed_title` alone, with the full-path Go-side check catching more).

`enrich.isAdultPath` re-runs the torrent-name parser over each path segment and short-circuits `tryAIFallback` when `TorrentInfo.Porn` fires on any segment. This skips **both** the Claude call **and** the `ai_enrich.query` write — the cache stays clean, and a re-run on the same path is just a microsecond-scale regex pass.

The `Porn` flag is set by patterns in `services/parse_torrent_name/main.go`:
- Adult studios / sites (Blacked, Brazzers, MyLF, OnlyFans, Manyvids, Hegre, JulesJordan, NubilesPorn, Stickam, Voyeur-russian, etc.)
- Explicit English keywords (porn, hentai, gangbang, stepmom/sis, creampie, hotwife, gloryhole, etc.)
- JAV studio code + numeric serial (`ABP-123`, `IPVR-00265`, `FC2PPV-1311003`, `SSIS-xxx`)
- BBC = "Big Black Cock" paired with an adult anchor (`bbc cock`, `bbc hungry`, `bbc treat`) — `BBC News` etc. don't match
- OnlyFans abbreviation `of - ` at the start of a title
- Bestiality phrases (`art of zoo`, `dog sex`, `zoo fuck`)
- Cam-girl "bate" + 6-digit timestamp convention (`bate 090607`)
- Russian explicit verbs (трах-, еб-, инцест, шлюх-, минет, дрочи, кримпай, пизд-, сперм-) with a non-Cyrillic prefix guard so `страх` (fear) doesn't false-match `трах`
- Chinese uncensored / leaked markers (`无码`, `無碼`, `流出`, `内射`, `中出`, `探花`, etc.)

Regression coverage: see `services/parse_torrent_name/main_test.go` adult-content section — 35 cases, half positives (one per pattern class), half negatives (Sex and the City, BBC News, Naughty Dog, Analyze This, Bates Motel, Страхование жизни) that must NOT trip.

The system prompt carries a duplicate "adult is out of scope" rule (`services/enrich/ai_resolver.go`) as second-line defense for paths that slip past the parser. Even if the parser misses, Claude returns `candidates: []` and the negative result is cached cheaply.

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

# Genres

A title's genres are a short list of slugs from one fixed vocabulary (`services/genre`). The same slugs serve every surface: the genre line on the torrent page (shown to everyone, like year and rating; never in JSON-LD), the genre line on library and Discover cards, and the library and Discover filters. A label is a locale key, `genre.<slug>`, so a genre reads the same on every page in every language.

Stored genres come from TMDB only. Discover's Cinemeta and addon metas carry English genre names, which the Discover client maps onto the same slugs.

## Vocabulary

31 slugs, in display order (`genre.Slugs`). A slug is a stored value and a locale key, so renaming one takes a migration. `TestEverySlugHasALabel` keeps `Slugs` and the `genre.<slug>` keys in `locales/en.json` one list. `TestEveryTMDBGenreIsASlug` checks the id map.

| slug | TMDB ids | addon strings | EN label |
|---|---|---|---|
| action | 28, 10759 | Action, Action & Adventure | Action |
| adventure | 12, 10759 | Adventure, Action & Adventure | Adventure |
| animation | 16 | Animation | Animation |
| biography | – | Biography | Biography |
| comedy | 35 | Comedy | Comedy |
| crime | 80 | Crime | Crime |
| documentary | 99 | Documentary | Documentary |
| drama | 18 | Drama | Drama |
| family | 10751 | Family | Family |
| fantasy | 14, 10765 | Fantasy, Sci-Fi & Fantasy | Fantasy |
| film_noir | – | Film-Noir | Film noir |
| game_show | – | Game-Show | Game show |
| history | 36 | History | History |
| horror | 27 | Horror | Horror |
| kids | 10762 | Kids | Kids |
| music | 10402 | Music | Music |
| musical | – | Musical | Musical |
| mystery | 9648 | Mystery | Mystery |
| news | 10763 | News | News |
| politics | 10768 | War & Politics | Politics |
| reality | 10764 | Reality-TV, Reality | Reality TV |
| romance | 10749 | Romance | Romance |
| scifi | 878, 10765 | Sci-Fi, Science Fiction, Sci-Fi & Fantasy | Sci-Fi |
| short | – | Short | Short film |
| soap | 10766 | Soap | Soap opera |
| sport | – | Sport | Sport |
| talk | 10767 | Talk-Show, Talk | Talk show |
| thriller | 53 | Thriller | Thriller |
| tv_movie | 10770 | TV Movie | TV movie |
| war | 10752, 10768 | War, War & Politics | War |
| western | 37 | Western | Western |

- **One id map for films and TV.** TMDB numbers its genres in one space. TV has a few combined genres, and each is split into its parts: 10759 into action + adventure, 10765 into scifi + fantasy, 10768 into war + politics. TMDB never gives a series 28, 12, 14, 878 or 10752, so without the split a library "Action" filter would match no series.
- **Only ids are read, not TMDB's names.** `GetDetails` sends no `language`, so the stored names are English. They are never shown, so that doesn't matter.
- **Mapping.** `genre.FromTMDB(metadata["genres"])` dedupes and keeps the order of first appearance. It skips unknown ids and malformed entries, and it never returns nil.
- **Addon strings** are matched case-sensitively and exactly. A string not in the table is shown as itself, never as a key, and is not counted as a slug. The IMDb names come from Cinemeta's `genre` options. The TMDB English names (`Science Fiction`, the `&` combinations, `Reality`, `Talk`, `Kids`, `Soap`, `TV Movie`) appear in Cinemeta metas that have only TMDB data.
- **Slugs TMDB lacks.** biography, film_noir, game_show, musical, short and sport have no TMDB id. They never appear in stored genres (library cards, torrent page) and come only from addon metas in Discover.

## Storage

Migration 75 adds a nullable `genres text[]` with no default to `public.movie_metadata` and `public.series_metadata`. On the Go side it is `models.VideoMetadata.Genres []string` (`pg:"genres,array"`).

| value | meaning | written by |
|---|---|---|
| `NULL` | not derived yet | a mapper with no genre data (OMDB, Kinopoisk); rows that existed before migration 75 |
| `'{}'` | derived: none | TMDB lists no genres, or the backfill found no TMDB row for the title |
| `'{drama,war}'` | derived | TMDB |

- **go-pg writes NULL only for a nil slice.** A nil slice becomes `DEFAULT`, which is NULL here. An empty slice becomes `'{}'`. On read, NULL scans as nil and `'{}'` as an empty slice. So code that derives genres must return a non-nil slice: `FromTMDB` does, and so does the backfill's `genresFor`.
- **An upsert without genres keeps the stored ones.** `UpsertMovieMetadata` and `UpsertSeriesMetadata` set `genres = COALESCE(EXCLUDED.genres, <table>.genres)`. The row is keyed on `video_id`, and the same video id is the same title. When OMDB or Kinopoisk rewrites the row (title, plot and poster are overwritten as before), TMDB's genres stay true, and NULL would only mean "this source doesn't know". A TMDB write, including `'{}'`, replaces them.
- **No index.** The library filters run over one user's rows, which the existing joins already narrow down.

## Sources

- **TMDB.** `TMDB.makeVideoMetadata` sets `Genres = genre.FromTMDB(info.Metadata["genres"])`. Every path that writes `*_metadata` goes through it: enrichment (`Map`, `MapByID` via `tryUpgrade`), the Discover watchlist and release subscriptions (`LookupByVideoID` → `MapByID`). The `tm.Info` it receives always carries the full `metadata`, whether it comes from `GetInfoByID` or `UpsertInfo`.
- **OMDB and Kinopoisk Unofficial leave `Genres` nil (v1 gap).** OMDB's `Genre` is an English comma-separated string. Kinopoisk's `genres` is Russian, lowercase and has no ids. Neither is read today. A title only these two know gets `'{}'` from the backfill, so it shows no genres and matches no genre filter.

## Backfill

`enrich popular` runs `Enricher.BackfillGenres` after the credits backfill. That cron runs every 6 h; see `docs/discover.md`, "Credits".

- **Flag:** `--genres-backfill-limit`, env `ENRICH_GENRES_BACKFILL_LIMIT`, default 100000; 0 disables.
- **Implementation:** `TMDB.BackfillGenres`, behind the `GenresBackfiller` capability, so it runs only where the TMDB mapper is configured. It makes no TMDB call: it reads only what `tmdb.info` already holds.
- **Rounds:** it takes 500 NULL rows at a time from `movie_metadata`, then from `series_metadata`. It stops once no NULL row is left, at the limit, or when a round writes nothing.
- **Matching rows to `tmdb.info`** (`genresFor`, which is pure and unit-tested):
  - `tt…` matches by `imdb_id`. That column is not unique in `tmdb.info`, so a row of the table's type wins (`movie_metadata` → type 1, `series_metadata` → type 2). If there is only a row of the other type, it is used: an IMDb id names one title, and `MapByID` would have stored the same one.
  - `tmdb<N>` matches by `tmdb_id`, and only a row of the table's type counts. TMDB numbers films and series separately and `tmdb.info` keys on the number alone, so a row of the other type is a different title. Such a row gets `'{}'`, the same rule as credits (`creditsFor`).
  - `kp…`, anything else, or no row gets `'{}'`.
- **The write** is one `UPDATE … FROM (VALUES …)` per round, and only to rows that are still NULL. A row that an enrichment filled between the read and the write keeps the newer value.
- **Cost per round:**
  - One scan of the metadata table for `genres IS NULL`. It has no index, but the table has no large jsonb either.
  - One read of `tmdb.info` through its indexes: the partial `idx_tmdb_info_imdb_id` (migration 59) and the PK, combined as a BitmapOr. This read decodes `metadata` only for the matched rows (at most 500 titles) and returns only `metadata->'genres'`; the whole column is 865 MB.
  - One UPDATE.
- **The first run** covers every existing row: 57k movie rows as of 2026-06. Today's count was not measured. Each round's scan starts from the beginning of the table, so the scans add up to about N/500 table scans; this is noted as a `ponytail:` comment.
- **Later runs** handle only the rows OMDB or Kinopoisk wrote since the last run, plus one empty scan per table.
- **The UPDATE bumps `updated_at`** through `trg_updated_at_movie_metadata` / `trg_updated_at_series_metadata`. As of 2026-10-31 nothing reads `*_metadata.updated_at`.
- **Known gap: `'{}'` is final for the backfill.** Suppose a title had no `tmdb.info` row when the backfill ran, and TMDB data reaches `tmdb.info` later by a path that doesn't rewrite the metadata row (`RefreshPopular`, a Discover `MapByID` for another surface). That title keeps `'{}'` until its next TMDB upsert.

## i18n

- **Keys:** `genre.<slug>` (31 labels), `genre.all` ("All genres"), `genre.label` ("Genre") and `genre.yearLabel` ("Year") in all 11 locales. The ambiguous English labels have `@genre.*` translator notes. `library.filterEmpty` and `library.filterReset` belong to the library filter.
- **Templates:** `{{ t $.Lang (printf "genre.%s" $slug) }}`.
- **Discover** imports `locales/<lang>.json?prefix=discover,genre`. The locale filter loader accepts a comma-separated prefix list, so there is no copy of the keys under `discover.*`; see `docs/i18n.md`, "Keys shared by templates and a JS bundle".

## Surfaces

### Library (`/lib`)

- `/lib/movies` and `/lib/series` take `?genre=<slug>`; a value `genre.IsSlug` rejects is ignored, the torrents section ignores it. SQL: `'<slug>' = ANY(md.genres)` (`models.LibraryFilter`), so NULL (not derived) and `'{}'` never match.
- Options are the distinct genres of the user's whole section (`models.GetLibraryGenres`), ignoring the genre and watched filters so they don't collapse; labelled with `genre.<slug>`, sorted by label with the page language's collation, "All genres" (`genre.all`) first. Hidden when the section has no genres. A selected genre stays listed even when no title has it any more.
- Sort, watched and genre are one GET form (`views/library/index.html`), so every change sends all three; the rating badges carry them too (`library/filter_inputs`).
- The form targets `main` with `data-async-no-scroll`, not `#list`: each change re-renders the sticky bar with the list, so Back and Forward restore the selects together with what they filter. A `#list` target left the selects at the newer values, and its history entry replayed on a multi-file resource page blanked that page's `#list`. The cost: the changed select loses keyboard focus.
- Filters that match nothing in a non-empty section show `library.filterEmpty` and a `library.filterReset` link (`?sort=<n>`: it clears the filters and keeps the sort); only a really empty section shows the "No torrents yet" state. The filter form stays; "Select" (bulk removal) is offered only when the list has items.
- Cards: up to 3 labels as the last line of the card body (`getCardGenres`), omitted when there are none. Same markup as Discover's `ItemGrid`.

### Torrent page

- `resource.Helper.GetEnrichedGenres` returns the stored slugs (via `getMetadata`), dropping any value `genre.IsSlug` rejects: a missing locale key would render as the key itself.
- One `<span>` at the end of the header's meta row (year, rating, Series badge, genres), up to 4 labels joined by ", ", omitted when there are none. It goes last because on a phone it wraps onto lines of its own; before the badge it would leave the badge alone on the line below, where it reads as one more genre.
- Shown to every visitor and on adult torrents, like year and rating (owner, 2026-10-30) — unlike credits, which are signed-in only.
- Never in JSON-LD (owner policy: no extra content schema); a template comment above the `ld+json` block says so. `handlers/resource/genres_test.go` checks it.

### Discover

- Cards and modal: `genreLabels` (`assets/src/js/lib/discover/genres.js`) maps an addon meta's `genres` (or `genre`) through the addon-string table onto `genre.<slug>` labels; a name outside the table shows as sent, never as a key. Cards show up to 3 (the library card's line), the stream modal up to 4 in its year/rating row, filled from `/meta` when the card had none.
- Filter: a select after the catalog select, built per catalog from its own manifest `genre` extra (`catalogFilter`) — optional with options → genres, "All genres" first; required with only 4-digit options → a year select defaulting to the newest year (Cinemeta's "New" catalogs carry the year in the genre extra); any other required extra → no select. The addon's own option string is sent, only the label is translated. Details: `docs/discover.md`, «Genre and year filter».
- Discover offers only what the addon offers: Cinemeta has no kids, soap, politics or tv_movie option, and does have biography, film_noir, game_show, musical, short and sport, which stored (TMDB) genres never carry — so those six never appear in the library filter.

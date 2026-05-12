-- Diagnostic columns on ai_enrich.query: track which resource and full
-- pathHint triggered each Claude normalization call. The composite
-- primary key (parsed_title, parsed_year, content_type) is shared across
-- resources, so resource_id stores the FIRST observed sample (whichever
-- enrichMediaInfo run hit the cache-miss path first) and hint_path
-- mirrors the full torrent file/folder path passed to AIResolver.
--
-- Both columns are nullable: legacy rows pre-this-migration carry NULL;
-- non-torrent flows (LookupByTitleYear) also stay NULL because they
-- never call AIResolver. UpsertQuery preserves prior values via
-- COALESCE so re-firing the same cache key from a different resource
-- doesn't overwrite the original diagnostic sample with churn.
ALTER TABLE ai_enrich.query
	ADD COLUMN resource_id text,
	ADD COLUMN hint_path   text;

-- Replace cache_index.path (full file path resolved against rest-api) with
-- file_idx (the Stremio addon's per-torrent file index). File index is
-- known cheaply at /stream time without any rest-api round-trip, while
-- path required a paginated ListResourceContent call per stream. The
-- column data is disposable cache, so we drop existing rows rather than
-- backfilling — the cache repopulates on the next play through
-- LinkResolver.ResolveLink → MarkAsCached.
TRUNCATE TABLE public.cache_index;

ALTER TABLE public.cache_index
	DROP CONSTRAINT cache_index_resource_path_backend_unique;

DROP INDEX public.cache_index_resource_path_idx;

ALTER TABLE public.cache_index
	DROP COLUMN path;

ALTER TABLE public.cache_index
	ADD COLUMN file_idx int NOT NULL;

ALTER TABLE public.cache_index
	ADD CONSTRAINT cache_index_resource_file_idx_backend_unique
		UNIQUE (resource_id, file_idx, backend_type);

CREATE INDEX cache_index_resource_file_idx_idx
	ON public.cache_index (resource_id, file_idx);

-- Disposable cache: the second source's rows go, the first one's stay.
DELETE FROM public.cache_index WHERE source <> 1;

ALTER TABLE public.cache_index
	DROP CONSTRAINT cache_index_resource_file_idx_backend_source_unique;

ALTER TABLE public.cache_index
	ADD CONSTRAINT cache_index_resource_file_idx_backend_unique
		UNIQUE (resource_id, file_idx, backend_type);

ALTER TABLE public.cache_index
	DROP COLUMN source;

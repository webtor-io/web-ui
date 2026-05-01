TRUNCATE TABLE public.cache_index;

ALTER TABLE public.cache_index
	DROP CONSTRAINT cache_index_resource_file_idx_backend_unique;

DROP INDEX public.cache_index_resource_file_idx_idx;

ALTER TABLE public.cache_index
	DROP COLUMN file_idx;

ALTER TABLE public.cache_index
	ADD COLUMN path text NOT NULL;

ALTER TABLE public.cache_index
	ADD CONSTRAINT cache_index_resource_path_backend_unique
		UNIQUE (resource_id, path, backend_type);

CREATE INDEX cache_index_resource_path_idx
	ON public.cache_index (resource_id, path);

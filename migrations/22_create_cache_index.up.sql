CREATE TABLE public.cache_index (
	cache_index_id uuid DEFAULT uuid_generate_v4() NOT NULL,
	backend_type text NOT NULL,
	resource_id text NOT NULL,
	path text NOT NULL,
	last_seen_at timestamptz DEFAULT now() NOT NULL,
	created_at timestamptz DEFAULT now() NOT NULL,
	updated_at timestamptz DEFAULT now() NOT NULL,
	CONSTRAINT cache_index_pk PRIMARY KEY (cache_index_id),
	CONSTRAINT cache_index_resource_path_backend_unique UNIQUE (resource_id, path, backend_type)
);

CREATE INDEX cache_index_resource_path_idx ON public.cache_index (resource_id, path);
CREATE INDEX cache_index_last_seen_at_idx ON public.cache_index (last_seen_at);

create trigger update_updated_at before
update
    on
    public.cache_index for each row execute function update_updated_at();

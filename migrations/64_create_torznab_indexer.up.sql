CREATE TABLE public.torznab_indexer (
	torznab_indexer_id uuid DEFAULT uuid_generate_v4() NOT NULL,
	"url" text NOT NULL,
	api_key text,
	user_id uuid NOT NULL,
	priority int2 DEFAULT 1 NOT NULL,
	enabled bool DEFAULT true NOT NULL,
	name text,
	caps jsonb,
	caps_fetched_at timestamptz,
	created_at timestamptz DEFAULT now() NOT NULL,
	updated_at timestamptz DEFAULT now() NOT NULL,
	CONSTRAINT torznab_indexer_pk PRIMARY KEY (torznab_indexer_id),
	CONSTRAINT torznab_indexer_unique UNIQUE (url, user_id),
	CONSTRAINT torznab_indexer_user_fk FOREIGN KEY (user_id)
		REFERENCES public."user" (user_id) ON DELETE CASCADE
);

CREATE INDEX torznab_indexer_user_priority_idx ON public.torznab_indexer (user_id, priority DESC) WHERE enabled = true;

create trigger update_updated_at before
update
    on
    public.torznab_indexer for each row execute function update_updated_at();

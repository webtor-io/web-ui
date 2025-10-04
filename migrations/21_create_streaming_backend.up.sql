CREATE TABLE public.streaming_backend (
	streaming_backend_id uuid DEFAULT uuid_generate_v4() NOT NULL,
	user_id uuid NOT NULL,
	type text NOT NULL,
	access_token text,
	config jsonb DEFAULT '{}' NOT NULL,
	priority smallint NOT NULL,
	proxied boolean DEFAULT false NOT NULL,
	enabled boolean DEFAULT true NOT NULL,
	last_status text,
	last_checked_at timestamptz,
	created_at timestamptz DEFAULT now() NOT NULL,
	updated_at timestamptz DEFAULT now() NOT NULL,
	CONSTRAINT streaming_backend_pk PRIMARY KEY (streaming_backend_id),
	CONSTRAINT streaming_backend_user_type_unique UNIQUE (user_id, type),
	CONSTRAINT streaming_backend_user_fk FOREIGN KEY (user_id)
		REFERENCES public."user" (user_id) ON DELETE CASCADE
);


create trigger update_updated_at before
update
    on
    public.streaming_backend for each row execute function update_updated_at();
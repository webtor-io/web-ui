CREATE TABLE public.stremio_settings (
	stremio_settings_id uuid DEFAULT uuid_generate_v4() NOT NULL,
	user_id uuid NOT NULL,
	settings jsonb NOT NULL,
	created_at timestamptz DEFAULT now() NOT NULL,
	updated_at timestamptz DEFAULT now() NOT NULL,
	CONSTRAINT stremio_settings_pk PRIMARY KEY (stremio_settings_id),
	CONSTRAINT stremio_settings_user_unique UNIQUE (user_id),
	CONSTRAINT stremio_settings_user_fk FOREIGN KEY (user_id)
		REFERENCES public."user" (user_id) ON DELETE CASCADE
);

create trigger update_updated_at before
update
    on
    public.stremio_settings for each row execute function update_updated_at();
CREATE TABLE public.addon_url (
	addon_url_id uuid DEFAULT uuid_generate_v4() NOT NULL,
	"url" text NOT NULL,
	user_id uuid NOT NULL,
	created_at timestamptz DEFAULT now() NOT NULL,
	updated_at timestamptz DEFAULT now() NOT NULL,
	CONSTRAINT addon_url_pk PRIMARY KEY (addon_url_id),
	CONSTRAINT addon_url_unique UNIQUE (url),
	CONSTRAINT addon_url_user_fk FOREIGN KEY (user_id)
		REFERENCES public."user" (user_id) ON DELETE CASCADE
);

create trigger update_updated_at before
update
    on
    public.addon_url for each row execute function update_updated_at();
CREATE TABLE public.notification (
	notification_id uuid DEFAULT uuid_generate_v4() NOT NULL,
	key text NOT NULL,
	title text NOT NULL,
	template text NOT NULL,
	body text NOT NULL,
	"to" text NOT NULL,
	created_at timestamptz DEFAULT now() NOT NULL,
	updated_at timestamptz DEFAULT now() NOT NULL,
	CONSTRAINT notification_pk PRIMARY KEY (notification_id)
);

CREATE INDEX notification_key_to_created_at_idx ON public.notification (key, "to", created_at DESC);

create trigger update_updated_at before
update
    on
    public.notification for each row execute function update_updated_at();

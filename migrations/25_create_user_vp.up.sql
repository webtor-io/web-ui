CREATE TABLE vault.user_vp (
	user_id uuid NOT NULL,
	total numeric NOT NULL,
	created_at timestamptz DEFAULT now() NOT NULL,
	updated_at timestamptz DEFAULT now() NOT NULL,
	CONSTRAINT user_vp_pk PRIMARY KEY (user_id),
	CONSTRAINT user_vp_user_fk FOREIGN KEY (user_id)
		REFERENCES public."user" (user_id) ON DELETE CASCADE
);

create trigger update_updated_at before
update
    on
    vault.user_vp for each row execute function update_updated_at();

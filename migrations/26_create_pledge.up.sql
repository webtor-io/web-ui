CREATE TABLE vault.pledge (
	pledge_id uuid DEFAULT uuid_generate_v4() NOT NULL,
	resource_id text NOT NULL,
	user_id uuid NOT NULL,
	amount numeric NOT NULL,
	funded bool DEFAULT true NOT NULL,
	frozen bool DEFAULT true NOT NULL,
	frozen_at timestamptz DEFAULT now() NOT NULL,
	created_at timestamptz DEFAULT now() NOT NULL,
	updated_at timestamptz DEFAULT now() NOT NULL,
	CONSTRAINT pledge_pk PRIMARY KEY (pledge_id),
	CONSTRAINT pledge_user_fk FOREIGN KEY (user_id)
		REFERENCES public."user" (user_id) ON DELETE CASCADE
);

create trigger update_updated_at before
update
    on
    vault.pledge for each row execute function update_updated_at();

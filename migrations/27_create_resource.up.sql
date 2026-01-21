CREATE TABLE vault.resource (
	resource_id text NOT NULL,
	required_vp numeric NOT NULL,
	funded_vp numeric NOT NULL,
	funded bool DEFAULT false NOT NULL,
	vaulted bool DEFAULT false NOT NULL,
	funded_at timestamptz,
	vaulted_at timestamptz,
	expired bool DEFAULT false NOT NULL,
	expired_at timestamptz,
	created_at timestamptz DEFAULT now() NOT NULL,
	updated_at timestamptz DEFAULT now() NOT NULL,
	CONSTRAINT resource_pk PRIMARY KEY (resource_id)
);

create trigger update_updated_at before
update
    on
    vault.resource for each row execute function update_updated_at();

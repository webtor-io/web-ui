CREATE TABLE vault.tx_log (
	tx_log_id uuid DEFAULT uuid_generate_v4() NOT NULL,
	user_id uuid NOT NULL,
	resource_id text,
	balance numeric NOT NULL,
	op_type smallint NOT NULL,
	created_at timestamptz DEFAULT now() NOT NULL,
	updated_at timestamptz DEFAULT now() NOT NULL,
	CONSTRAINT tx_log_pk PRIMARY KEY (tx_log_id),
	CONSTRAINT tx_log_user_fk FOREIGN KEY (user_id)
		REFERENCES public."user" (user_id) ON DELETE CASCADE
);

create trigger update_updated_at before
update
    on
    vault.tx_log for each row execute function update_updated_at();

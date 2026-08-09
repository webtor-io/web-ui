CREATE TABLE public.device_auth (
	device_code uuid DEFAULT uuid_generate_v4() NOT NULL,
	user_code varchar(16) NOT NULL,
	user_id uuid,
	token uuid,
	device_name varchar(64),
	status varchar(16) DEFAULT 'pending' NOT NULL,
	expires_at timestamptz NOT NULL,
	created_at timestamptz DEFAULT now() NOT NULL,
	updated_at timestamptz DEFAULT now() NOT NULL,
	CONSTRAINT device_auth_pk PRIMARY KEY (device_code),
	CONSTRAINT device_auth_user_code_unique UNIQUE (user_code),
	CONSTRAINT device_auth_user_fk FOREIGN KEY (user_id)
		REFERENCES public."user" (user_id) ON DELETE CASCADE
);

create trigger update_updated_at before
update
    on
    public.device_auth for each row execute function update_updated_at();

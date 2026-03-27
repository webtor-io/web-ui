CREATE TABLE public.watch_history (
	user_id		uuid		NOT NULL,
	resource_id	text		NOT NULL,
	path		text		NOT NULL,
	position	real		NOT NULL DEFAULT 0,
	duration	real		NOT NULL DEFAULT 0,
	watched		boolean		NOT NULL DEFAULT false,
	created_at	timestamptz	NOT NULL DEFAULT now(),
	updated_at	timestamptz	NOT NULL DEFAULT now(),

	CONSTRAINT watch_history_pk PRIMARY KEY (user_id, resource_id, path),
	CONSTRAINT watch_history_user_fk FOREIGN KEY (user_id)
		REFERENCES public."user"(user_id)
		ON DELETE CASCADE
);

CREATE INDEX watch_history_user_updated_idx ON public.watch_history (user_id, updated_at DESC);

CREATE TRIGGER update_watch_history_updated_at
	BEFORE UPDATE ON public.watch_history
	FOR EACH ROW EXECUTE FUNCTION update_updated_at();

-- User-uploaded subtitle tracks attached to a file inside a torrent.
-- The binary lives in S3 under its SHA-256 hash (dedup across users); this
-- table stores the per-user binding to a specific (resource_id, path).
CREATE TABLE public.user_subtitle (
	user_subtitle_id	uuid		NOT NULL DEFAULT uuid_generate_v4(),
	user_id			uuid		NOT NULL,
	resource_id		text		NOT NULL,
	path			text		NOT NULL,
	hash			text		NOT NULL,
	original_name		text		NOT NULL,
	format			text		NOT NULL,
	size			bigint		NOT NULL,
	created_at		timestamptz	NOT NULL DEFAULT now(),
	updated_at		timestamptz	NOT NULL DEFAULT now(),

	CONSTRAINT user_subtitle_pk PRIMARY KEY (user_subtitle_id),
	CONSTRAINT user_subtitle_unique UNIQUE (user_id, resource_id, path, hash),
	CONSTRAINT user_subtitle_user_fk FOREIGN KEY (user_id)
		REFERENCES public."user"(user_id)
		ON DELETE CASCADE
);

CREATE INDEX user_subtitle_lookup_idx
	ON public.user_subtitle (user_id, resource_id, path);

CREATE INDEX user_subtitle_hash_idx
	ON public.user_subtitle (hash);

CREATE TRIGGER update_user_subtitle_updated_at
	BEFORE UPDATE ON public.user_subtitle
	FOR EACH ROW EXECUTE FUNCTION update_updated_at();

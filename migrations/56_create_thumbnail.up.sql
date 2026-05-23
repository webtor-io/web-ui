-- Per-resource thumbnail generated during enrichment when no IMDb poster
-- is available. The binary lives in S3 under its SHA-1 hash (dedup across
-- resources that happen to ship the same poster.jpg). source_kind enum is
-- a smallint (1=image_file, 2=ffmpeg_frame) for grep-friendly debugging
-- without the storage cost of a text column.
CREATE TABLE public.thumbnail (
	thumbnail_id	uuid		NOT NULL DEFAULT uuid_generate_v4(),
	resource_id	text		NOT NULL,
	path		text		NOT NULL,
	-- 0 for image-file extracts (folder.jpg / cover.jpg / etc), N seconds
	-- for ffmpeg-frame extracts. Part of the composite uniqueness key so
	-- the same torrent path can host multiple ffmpeg-frame offsets without
	-- conflict.
	offset_sec	integer		NOT NULL DEFAULT 0,
	source_kind	smallint	NOT NULL,
	hash		text		NOT NULL,
	format		text		NOT NULL,
	size		bigint		NOT NULL,
	width		integer,
	height		integer,
	created_at	timestamptz	NOT NULL DEFAULT now(),
	updated_at	timestamptz	NOT NULL DEFAULT now(),

	CONSTRAINT thumbnail_pk PRIMARY KEY (thumbnail_id),
	CONSTRAINT thumbnail_unique UNIQUE (resource_id, path, offset_sec)
);

CREATE INDEX thumbnail_resource_id_idx
	ON public.thumbnail (resource_id);

CREATE INDEX thumbnail_hash_idx
	ON public.thumbnail (hash);

CREATE TRIGGER update_thumbnail_updated_at
	BEFORE UPDATE ON public.thumbnail
	FOR EACH ROW EXECUTE FUNCTION update_updated_at();

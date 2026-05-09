ALTER TABLE public.stremio_addon_url
	ADD COLUMN manifest_id text,
	ADD COLUMN name text,
	ADD COLUMN manifest_version text,
	ADD COLUMN manifest_resources jsonb,
	ADD COLUMN manifest_types jsonb,
	ADD COLUMN manifest_logo text,
	ADD COLUMN manifest_fetched_at timestamptz;

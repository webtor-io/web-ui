ALTER TABLE public.stremio_addon_url
	DROP COLUMN IF EXISTS manifest_id,
	DROP COLUMN IF EXISTS name,
	DROP COLUMN IF EXISTS manifest_version,
	DROP COLUMN IF EXISTS manifest_resources,
	DROP COLUMN IF EXISTS manifest_types,
	DROP COLUMN IF EXISTS manifest_logo,
	DROP COLUMN IF EXISTS manifest_fetched_at;

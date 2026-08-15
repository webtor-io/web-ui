ALTER TABLE public.release_subscription
	DROP COLUMN IF EXISTS preferred_resolutions,
	DROP COLUMN IF EXISTS preferred_language;

DROP INDEX IF EXISTS public.stremio_addon_url_user_priority_idx;

ALTER TABLE public.stremio_addon_url DROP COLUMN IF EXISTS enabled;
ALTER TABLE public.stremio_addon_url DROP COLUMN IF EXISTS priority;

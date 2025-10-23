ALTER TABLE public.stremio_addon_url ADD COLUMN priority int2 DEFAULT 1 NOT NULL;
ALTER TABLE public.stremio_addon_url ADD COLUMN enabled bool DEFAULT true NOT NULL;

CREATE INDEX stremio_addon_url_user_priority_idx ON public.stremio_addon_url (user_id, priority DESC) WHERE enabled = true;

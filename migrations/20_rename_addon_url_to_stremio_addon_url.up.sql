ALTER TABLE public.addon_url RENAME TO stremio_addon_url;

ALTER TABLE public.stremio_addon_url RENAME COLUMN addon_url_id TO stremio_addon_url_id;

ALTER TABLE public.stremio_addon_url RENAME CONSTRAINT addon_url_pk TO stremio_addon_url_pk;
ALTER TABLE public.stremio_addon_url RENAME CONSTRAINT addon_url_unique TO stremio_addon_url_unique;
ALTER TABLE public.stremio_addon_url RENAME CONSTRAINT addon_url_user_fk TO stremio_addon_url_user_fk;

DROP TRIGGER update_updated_at ON public.stremio_addon_url;
create trigger update_updated_at before
update
    on
    public.stremio_addon_url for each row execute function update_updated_at();
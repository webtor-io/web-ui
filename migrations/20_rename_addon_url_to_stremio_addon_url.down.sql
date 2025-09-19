ALTER TABLE public.stremio_addon_url RENAME TO addon_url;

ALTER TABLE public.addon_url RENAME COLUMN stremio_addon_url_id TO addon_url_id;

ALTER TABLE public.addon_url RENAME CONSTRAINT stremio_addon_url_pk TO addon_url_pk;
ALTER TABLE public.addon_url RENAME CONSTRAINT stremio_addon_url_unique TO addon_url_unique;
ALTER TABLE public.addon_url RENAME CONSTRAINT stremio_addon_url_user_fk TO addon_url_user_fk;

DROP TRIGGER update_updated_at ON public.addon_url;
create trigger update_updated_at before
update
    on
    public.addon_url for each row execute function update_updated_at();
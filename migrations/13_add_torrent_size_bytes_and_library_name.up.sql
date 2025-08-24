-- Add torrent_size_bytes column to torrent_resource table
ALTER TABLE public.torrent_resource
    ADD COLUMN IF NOT EXISTS torrent_size_bytes bigint;

-- Add name column to library table
ALTER TABLE public.library
    ADD COLUMN IF NOT EXISTS name text;
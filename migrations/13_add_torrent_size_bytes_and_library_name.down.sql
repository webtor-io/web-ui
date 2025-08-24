-- Remove name column from library table
ALTER TABLE public.library
    DROP COLUMN IF EXISTS name;

-- Remove torrent_size_bytes column from torrent_resource table
ALTER TABLE public.torrent_resource
    DROP COLUMN IF EXISTS torrent_size_bytes;
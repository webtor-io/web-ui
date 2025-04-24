CREATE TABLE public.torrent_resource (
     resource_id text PRIMARY KEY,
     name text NOT NULL,
     file_count integer NOT NULL,
     size_bytes bigint NOT NULL,
     created_at timestamptz DEFAULT now() NOT NULL
);
-- Persist the torrent file index alongside path on movie/episode rows.
-- The Stremio Library stream service previously re-derived this index on
-- every /stream request by paginating rest-api's /resource/<hash>/list and
-- counting files until the path matched — 2-3 sequential rest-api round
-- trips for files deep in large season packs, which under the 5s
-- CompositeStream timeout intermittently dropped the whole Library result
-- (vault streams silently missing in Stremio). Storing file_idx at scan
-- time makes Library stream assembly a pure DB read.
--
-- Nullable: pre-existing rows keep file_idx NULL and fall back to the old
-- list-walk in Library.getStreamItem until re-enriched.
-- file_size (bytes) is captured in the same scan from rest-api's
-- ListItem.Size so the Stremio Library stream description can show "💾 1.41
-- GB" without a /stream-time round trip.
ALTER TABLE public.movie
	ADD COLUMN file_idx integer,
	ADD COLUMN file_size bigint;

ALTER TABLE public.episode
	ADD COLUMN file_idx integer,
	ADD COLUMN file_size bigint;

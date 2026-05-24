-- Per-resource classification + parsed-name cache. Sits orthogonal to
-- movie_metadata / series_metadata: those exist only when enrichment
-- matched a known work; this row exists for every torrent we've seen
-- and carries the parse_torrent_name output plus derived flags.
--
-- is_adult / is_sport are denormalised from `metadata` for hot-path
-- queries (poster blur decision, library filters). Partial indexes
-- on `true` only — the false case is the default and doesn't need
-- to be indexed.
--
-- `metadata` is the full ptn.TorrentInfo JSON: resolution, codec,
-- audio, quality, season/episode, etc. Stored as jsonb so we can
-- iterate later (showing resolution/codec on cards, building filters)
-- without further migrations.
--
-- FK ON DELETE CASCADE mirrors movie/series/episode — when a torrent
-- record is wiped (e.g. via abuse-store ban broadcast), classification
-- evaporates with it.
CREATE TABLE public.resource_metadata (
	resource_id	text		NOT NULL REFERENCES media_info (resource_id) ON DELETE CASCADE,
	is_adult	boolean		NOT NULL DEFAULT false,
	is_sport	boolean		NOT NULL DEFAULT false,
	metadata	jsonb,
	created_at	timestamptz	NOT NULL DEFAULT now(),
	updated_at	timestamptz	NOT NULL DEFAULT now(),

	CONSTRAINT resource_metadata_pk PRIMARY KEY (resource_id)
);

CREATE INDEX resource_metadata_is_adult_idx
	ON public.resource_metadata (resource_id)
	WHERE is_adult = true;

CREATE INDEX resource_metadata_is_sport_idx
	ON public.resource_metadata (resource_id)
	WHERE is_sport = true;

CREATE TRIGGER update_resource_metadata_updated_at
	BEFORE UPDATE ON public.resource_metadata
	FOR EACH ROW EXECUTE FUNCTION update_updated_at();

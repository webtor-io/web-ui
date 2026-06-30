-- The Stremio Library series stream/meta path loads a series' episodes via
-- go-pg's Relation("Episodes") => `SELECT ... FROM episode WHERE series_id = ?`.
-- Without an index on episode.series_id that is a full sequential scan of the
-- whole episode table (millions of rows) on EVERY series request — ~21s in
-- prod — which blew past the CompositeStream 5s timeout and intermittently
-- dropped the entire Library result (vault streams missing in Stremio).
-- Index Scan brings it to sub-millisecond.
CREATE INDEX IF NOT EXISTS idx_episode_series_id
	ON public.episode (series_id);

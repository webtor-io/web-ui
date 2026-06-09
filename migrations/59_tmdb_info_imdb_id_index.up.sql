-- Discover batch localization looks up tmdb.info by imdb_id for every
-- catalog item (up to 100 per request). GetTmdbID / resolveLocalizeIDs
-- did sequential scans before this index.
CREATE INDEX IF NOT EXISTS idx_tmdb_info_imdb_id
	ON tmdb.info (imdb_id)
	WHERE imdb_id IS NOT NULL;

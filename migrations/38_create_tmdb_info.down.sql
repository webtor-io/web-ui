DROP TRIGGER IF EXISTS trg_updated_at_tmdb_season_info ON tmdb.season_info;
DROP FUNCTION IF EXISTS tmdb.update_season_info_updated_at_column();
DROP TABLE IF EXISTS tmdb.season_info;

DROP TRIGGER IF EXISTS trg_updated_at_tmdb_query ON tmdb.query;
DROP FUNCTION IF EXISTS tmdb.update_query_updated_at_column();
DROP INDEX IF EXISTS uq_tmdb_query_key;
DROP TABLE IF EXISTS tmdb.query;

DROP TRIGGER IF EXISTS trg_updated_at_tmdb_info ON tmdb.info;
DROP FUNCTION IF EXISTS tmdb.update_info_updated_at_column();
DROP TABLE IF EXISTS tmdb.info;

DROP SCHEMA IF EXISTS tmdb CASCADE;

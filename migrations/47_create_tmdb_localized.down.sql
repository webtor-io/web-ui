DROP TRIGGER IF EXISTS trg_updated_at_tmdb_localized ON tmdb.localized;
DROP FUNCTION IF EXISTS tmdb.update_localized_updated_at_column();
DROP TABLE IF EXISTS tmdb.localized;

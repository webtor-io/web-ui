-- Drop indexes
DROP INDEX IF EXISTS idx_movie_resource_id;
DROP INDEX IF EXISTS idx_series_resource_id;

-- Drop triggers
DROP TRIGGER IF EXISTS trg_set_updated_at_movie ON movie;
DROP TRIGGER IF EXISTS trg_set_updated_at_series ON series;
DROP TRIGGER IF EXISTS trg_set_updated_at_episode ON episode;
DROP TRIGGER IF EXISTS trg_updated_at_movie_metadata ON movie_metadata;
DROP TRIGGER IF EXISTS trg_updated_at_series_metadata ON series_metadata;

-- Drop trigger functions
DROP FUNCTION IF EXISTS update_movie_updated_at_column;
DROP FUNCTION IF EXISTS update_series_updated_at_column;
DROP FUNCTION IF EXISTS update_episode_updated_at_column;
DROP FUNCTION IF EXISTS update_movie_metadata_updated_at_column;
DROP FUNCTION IF EXISTS update_series_metadata_updated_at_column;

-- Drop tables
DROP TABLE IF EXISTS episode;
DROP TABLE IF EXISTS series;
DROP TABLE IF EXISTS movie;
DROP TABLE IF EXISTS movie_metadata;
DROP TABLE IF EXISTS series_metadata;
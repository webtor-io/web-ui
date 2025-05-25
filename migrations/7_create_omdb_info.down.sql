-- Drop triggers
DROP TRIGGER IF EXISTS trg_updated_at_info ON omdb.info;
DROP TRIGGER IF EXISTS trg_updated_at_query ON omdb.query;

-- Drop functions
DROP FUNCTION IF EXISTS omdb.update_info_updated_at_column;
DROP FUNCTION IF EXISTS omdb.update_query_updated_at_column;

-- Drop index
DROP INDEX IF EXISTS uq_query_key;

-- Drop tables
DROP TABLE IF EXISTS omdb.info;
DROP TABLE IF EXISTS omdb.query;

--- Drop schema
DROP SCHEMA omdb;

DROP TRIGGER IF EXISTS trg_updated_at_kinopoisk_query ON kinopoisk_unofficial.query;
DROP FUNCTION IF EXISTS kinopoisk_unofficial.update_query_updated_at_column();
DROP INDEX IF EXISTS uq_kinopoisk_query_key;
DROP TABLE IF EXISTS kinopoisk_unofficial.query;

DROP TRIGGER IF EXISTS trg_updated_at_kinopoisk_info ON kinopoisk_unofficial.info;
DROP FUNCTION IF EXISTS kinopoisk_unofficial.update_info_updated_at_column();
DROP TABLE IF EXISTS kinopoisk_unofficial.info;

DROP SCHEMA IF EXISTS kinopoisk_unofficial CASCADE;
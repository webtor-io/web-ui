DROP INDEX IF EXISTS idx_media_info_status;
DROP INDEX IF EXISTS idx_media_info_created_at;

DROP TRIGGER IF EXISTS trg_set_updated_at ON media_info;
DROP FUNCTION IF EXISTS update_media_info_updated_at_column;
DROP TABLE IF EXISTS media_info;
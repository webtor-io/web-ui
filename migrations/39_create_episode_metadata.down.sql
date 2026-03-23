ALTER TABLE episode
	DROP COLUMN IF EXISTS episode_metadata_id;

DROP TRIGGER IF EXISTS trg_updated_at_episode_metadata ON episode_metadata;
DROP FUNCTION IF EXISTS update_episode_metadata_updated_at_column();
DROP TABLE IF EXISTS episode_metadata;

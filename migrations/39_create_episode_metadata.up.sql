-- Episode metadata
CREATE TABLE episode_metadata
(
	episode_metadata_id UUID PRIMARY KEY     DEFAULT uuid_generate_v4(),
	video_id            TEXT        NOT NULL, -- series IMDB/TMDB ID
	season              SMALLINT    NOT NULL,
	episode             SMALLINT    NOT NULL,
	title               TEXT,
	plot                TEXT,
	still_url           TEXT,
	air_date            DATE,
	rating              NUMERIC(3, 1),
	created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Unique index on (video_id, season, episode)
CREATE UNIQUE INDEX episode_metadata_video_season_episode_unique
	ON episode_metadata (video_id, season, episode);

-- Auto-update updated_at on change
CREATE FUNCTION update_episode_metadata_updated_at_column()
	RETURNS TRIGGER AS
$$
BEGIN
	NEW.updated_at = now();
	RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_updated_at_episode_metadata
	BEFORE UPDATE
	ON episode_metadata
	FOR EACH ROW
EXECUTE FUNCTION update_episode_metadata_updated_at_column();

-- Add FK from episode to episode_metadata
ALTER TABLE episode
	ADD COLUMN episode_metadata_id UUID REFERENCES episode_metadata (episode_metadata_id) ON DELETE SET NULL;

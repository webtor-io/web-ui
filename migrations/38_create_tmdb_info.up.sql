-- Schema for TMDB metadata
CREATE SCHEMA IF NOT EXISTS tmdb;

-- TMDB metadata cache
CREATE TABLE tmdb.info
(
	tmdb_id    INTEGER PRIMARY KEY,
	imdb_id    TEXT,
	title      TEXT        NOT NULL,
	year       SMALLINT,
	type       SMALLINT    NOT NULL, -- 1=movie, 2=series
	metadata   JSONB       NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Trigger function to auto-update updated_at
CREATE FUNCTION tmdb.update_info_updated_at_column()
	RETURNS TRIGGER AS
$$
BEGIN
	NEW.updated_at = now();
	RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger on update
CREATE TRIGGER trg_updated_at_tmdb_info
	BEFORE UPDATE
	ON tmdb.info
	FOR EACH ROW
EXECUTE FUNCTION tmdb.update_info_updated_at_column();

-- Query table for caching search results
CREATE TABLE tmdb.query
(
	query_id   UUID PRIMARY KEY     DEFAULT uuid_generate_v4(),
	title      TEXT        NOT NULL,
	year       SMALLINT,
	type       SMALLINT    NOT NULL, -- 1=movie, 2=series
	tmdb_id    INTEGER,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Unique index for query de-duplication
CREATE UNIQUE INDEX uq_tmdb_query_key
	ON tmdb.query (title, COALESCE(year, -1), type)
	WHERE title IS NOT NULL;

-- Trigger function for updated_at
CREATE FUNCTION tmdb.update_query_updated_at_column()
	RETURNS TRIGGER AS
$$
BEGIN
	NEW.updated_at = now();
	RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger for update on query
CREATE TRIGGER trg_updated_at_tmdb_query
	BEFORE UPDATE
	ON tmdb.query
	FOR EACH ROW
EXECUTE FUNCTION tmdb.update_query_updated_at_column();

-- Season info cache (full season episodes data)
CREATE TABLE tmdb.season_info
(
	tmdb_id    INTEGER     NOT NULL,
	season     SMALLINT    NOT NULL,
	metadata   JSONB       NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	PRIMARY KEY (tmdb_id, season)
);

-- Trigger function to auto-update updated_at
CREATE FUNCTION tmdb.update_season_info_updated_at_column()
	RETURNS TRIGGER AS
$$
BEGIN
	NEW.updated_at = now();
	RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger on update
CREATE TRIGGER trg_updated_at_tmdb_season_info
	BEFORE UPDATE
	ON tmdb.season_info
	FOR EACH ROW
EXECUTE FUNCTION tmdb.update_season_info_updated_at_column();

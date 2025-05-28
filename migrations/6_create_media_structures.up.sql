-- Movie metadata
CREATE TABLE movie_metadata
(
    movie_metadata_id UUID PRIMARY KEY     DEFAULT uuid_generate_v4(),
    video_id          TEXT UNIQUE,
    title             TEXT        NOT NULL,
    year              SMALLINT,
    plot              TEXT,
    poster_url        TEXT,
    rating            NUMERIC(3, 1),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE FUNCTION update_movie_metadata_updated_at_column()
    RETURNS TRIGGER AS
$$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_updated_at_movie_metadata
    BEFORE UPDATE
    ON movie_metadata
    FOR EACH ROW
EXECUTE FUNCTION update_movie_metadata_updated_at_column();


-- Series metadata
CREATE TABLE series_metadata
(
    series_metadata_id UUID PRIMARY KEY     DEFAULT uuid_generate_v4(),
    video_id           TEXT UNIQUE,
    title              TEXT        NOT NULL,
    year               SMALLINT,
    plot               TEXT,
    poster_url         TEXT,
    rating             NUMERIC(3, 1),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE FUNCTION update_series_metadata_updated_at_column()
    RETURNS TRIGGER AS
$$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_updated_at_series_metadata
    BEFORE UPDATE
    ON series_metadata
    FOR EACH ROW
EXECUTE FUNCTION update_series_metadata_updated_at_column();
-- Table: movie
CREATE TABLE movie
(
    movie_id          UUID PRIMARY KEY     DEFAULT uuid_generate_v4(),
    resource_id       TEXT        NOT NULL REFERENCES media_info (resource_id) ON DELETE CASCADE,
    movie_metadata_id uuid        REFERENCES movie_metadata (movie_metadata_id) ON DELETE SET NULL,
    title             TEXT        NOT NULL,
    year              SMALLINT,
    path              TEXT,
    metadata          JSONB,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Index to optimize lookup by resource_id
CREATE INDEX idx_movie_resource_id ON movie (resource_id);

-- Auto-update updated_at on change
CREATE FUNCTION update_movie_updated_at_column()
    RETURNS TRIGGER AS
$$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_set_updated_at_movie
    BEFORE UPDATE
    ON movie
    FOR EACH ROW
EXECUTE FUNCTION update_movie_updated_at_column();

-- Table: series
CREATE TABLE series
(
    series_id          UUID PRIMARY KEY     DEFAULT uuid_generate_v4(),
    resource_id        TEXT        NOT NULL REFERENCES media_info (resource_id) ON DELETE CASCADE,
    series_metadata_id uuid        REFERENCES series_metadata (series_metadata_id) ON DELETE SET NULL,
    title              TEXT        NOT NULL,
    year               SMALLINT,
    metadata           JSONB,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Index to optimize lookup by resource_id
CREATE INDEX idx_series_resource_id ON series (resource_id);

-- Auto-update updated_at on change
CREATE FUNCTION update_series_updated_at_column()
    RETURNS TRIGGER AS
$$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_set_updated_at_series
    BEFORE UPDATE
    ON series
    FOR EACH ROW
EXECUTE FUNCTION update_series_updated_at_column();

-- Table: episode
CREATE TABLE episode
(
    episode_id  UUID PRIMARY KEY     DEFAULT uuid_generate_v4(),
    series_id   UUID        NOT NULL REFERENCES series (series_id) ON DELETE CASCADE,
    season      SMALLINT,
    episode     SMALLINT,
    resource_id TEXT        NOT NULL REFERENCES media_info (resource_id) ON DELETE CASCADE,
    title       TEXT,
    path        TEXT,
    metadata    JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Auto-update updated_at on change
CREATE FUNCTION update_episode_updated_at_column()
    RETURNS TRIGGER AS
$$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_set_updated_at_episode
    BEFORE UPDATE
    ON episode
    FOR EACH ROW
EXECUTE FUNCTION update_episode_updated_at_column();
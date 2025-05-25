-- Schema for Kinopoisk unofficial metadata
CREATE SCHEMA IF NOT EXISTS kinopoisk_unofficial;

-- Kinopoisk metadata cache
CREATE TABLE kinopoisk_unofficial.info
(
    kp_id      INTEGER PRIMARY KEY, -- kinopoisk filmId
    imdb_id    TEXT,
    title      TEXT        NOT NULL,
    year       SMALLINT,
    metadata   JSONB       NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Trigger function to auto-update updated_at
CREATE FUNCTION kinopoisk_unofficial.update_info_updated_at_column()
    RETURNS TRIGGER AS
$$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger on update
CREATE TRIGGER trg_updated_at_kinopoisk_info
    BEFORE UPDATE
    ON kinopoisk_unofficial.info
    FOR EACH ROW
EXECUTE FUNCTION kinopoisk_unofficial.update_info_updated_at_column();

-- Query table for caching search results
CREATE TABLE kinopoisk_unofficial.query
(
    query_id   UUID PRIMARY KEY     DEFAULT uuid_generate_v4(),
    title      TEXT        NOT NULL,
    year       SMALLINT,
    kp_id      INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Partial unique index for query de-duplication
CREATE UNIQUE INDEX uq_kinopoisk_query_key
    ON kinopoisk_unofficial.query (title, COALESCE(year, -1))
    WHERE title IS NOT NULL;

-- Trigger function for updated_at
CREATE FUNCTION kinopoisk_unofficial.update_query_updated_at_column()
    RETURNS TRIGGER AS
$$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger for update on query
CREATE TRIGGER trg_updated_at_kinopoisk_query
    BEFORE UPDATE
    ON kinopoisk_unofficial.query
    FOR EACH ROW
EXECUTE FUNCTION kinopoisk_unofficial.update_query_updated_at_column();

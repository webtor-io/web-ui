CREATE schema omdb;

-- OMDB metadata cache
CREATE TABLE omdb.info (
    imdb_id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    year smallint,
    type SMALLINT NOT NULL, -- 1=movie, 2=series, 3=episode
    metadata JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Triggers
CREATE FUNCTION omdb.update_info_updated_at_column()
    RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_updated_at_info
    BEFORE UPDATE ON omdb.info
    FOR EACH ROW EXECUTE FUNCTION omdb.update_info_updated_at_column();

-- create omdb_query table
CREATE TABLE omdb.query (
    query_id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    title TEXT NOT NULL,
    year SMALLINT,
    type SMALLINT NOT NULL, -- 1 = movie, 2 = series
    imdb_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- unique index on (title, year, type)
CREATE UNIQUE INDEX uq_query_key
    ON omdb.query (title, COALESCE(year, -1), type);

-- updated_at trigger
CREATE FUNCTION omdb.update_query_updated_at_column()
    RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_updated_at_query
    BEFORE UPDATE ON omdb.query
    FOR EACH ROW
EXECUTE FUNCTION omdb.update_query_updated_at_column();
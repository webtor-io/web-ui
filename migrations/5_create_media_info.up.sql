CREATE TABLE media_info (
    resource_id TEXT PRIMARY KEY,
    status SMALLINT NOT NULL,
    has_movie BOOLEAN NOT NULL DEFAULT FALSE,
    has_series BOOLEAN NOT NULL DEFAULT FALSE,
    media_count SMALLINT NOT NULL DEFAULT 0,
    error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE FUNCTION update_media_info_updated_at_column()
    RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_set_updated_at
    BEFORE UPDATE ON media_info
    FOR EACH ROW
EXECUTE FUNCTION update_media_info_updated_at_column();

CREATE INDEX idx_media_info_status ON media_info(status);
CREATE INDEX idx_media_info_created_at ON media_info(created_at);
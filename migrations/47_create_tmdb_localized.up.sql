-- Lightweight cache for localized TMDB title + overview.
-- PK is (tmdb_id, lang) so each film × language is one row.
CREATE TABLE tmdb.localized
(
	tmdb_id    INTEGER     NOT NULL,
	lang       TEXT        NOT NULL,
	title      TEXT        NOT NULL,
	plot       TEXT        NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	PRIMARY KEY (tmdb_id, lang)
);

-- Trigger function to auto-update updated_at
CREATE FUNCTION tmdb.update_localized_updated_at_column()
	RETURNS TRIGGER AS
$$
BEGIN
	NEW.updated_at = now();
	RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger on update
CREATE TRIGGER trg_updated_at_tmdb_localized
	BEFORE UPDATE
	ON tmdb.localized
	FOR EACH ROW
EXECUTE FUNCTION tmdb.update_localized_updated_at_column();

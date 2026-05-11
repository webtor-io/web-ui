CREATE SCHEMA IF NOT EXISTS ai_enrich;

CREATE TABLE ai_enrich.query (
	parsed_title text        NOT NULL,
	-- -1 sentinel represents "no parsed year" so the (title, year, ct)
	-- composite key behaves like a regular UNIQUE without NULL-distinct
	-- semantics tripping up cache lookups and ON CONFLICT.
	parsed_year  smallint    NOT NULL DEFAULT -1,
	content_type smallint    NOT NULL,
	candidates   jsonb       NOT NULL DEFAULT '[]'::jsonb,
	model        text,
	created_at   timestamptz NOT NULL DEFAULT now(),
	updated_at   timestamptz NOT NULL DEFAULT now(),
	CONSTRAINT ai_enrich_query_pk PRIMARY KEY (parsed_title, parsed_year, content_type)
);

CREATE FUNCTION ai_enrich.update_query_updated_at_column()
	RETURNS TRIGGER AS
$$
BEGIN
	NEW.updated_at = now();
	RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_updated_at_ai_enrich_query
	BEFORE UPDATE
	ON ai_enrich.query
	FOR EACH ROW
EXECUTE FUNCTION ai_enrich.update_query_updated_at_column();

CREATE TABLE url_alias
(
    code       TEXT PRIMARY KEY,
    url        TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
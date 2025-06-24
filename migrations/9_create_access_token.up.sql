CREATE TABLE access_token
(
    token      UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES "user"(user_id) ON DELETE CASCADE,
    name       TEXT        NOT NULL,
    scope      TEXT[]      NOT NULL DEFAULT '{}',
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, name)
);
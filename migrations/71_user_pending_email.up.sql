ALTER TABLE public."user"
    ADD COLUMN pending_email            text,
    ADD COLUMN pending_email_token      text,
    ADD COLUMN pending_email_expires_at timestamptz;

CREATE UNIQUE INDEX user_pending_email_token_idx
    ON public."user" (pending_email_token)
    WHERE pending_email_token IS NOT NULL;

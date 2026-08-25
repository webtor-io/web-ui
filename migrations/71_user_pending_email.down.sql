DROP INDEX IF EXISTS user_pending_email_token_idx;

ALTER TABLE public."user"
    DROP COLUMN pending_email_expires_at,
    DROP COLUMN pending_email_token,
    DROP COLUMN pending_email,
    DROP COLUMN notification_email;

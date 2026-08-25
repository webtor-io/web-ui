DROP INDEX IF EXISTS notification_key_user_created_idx;
DROP INDEX IF EXISTS notification_user_created_idx;

DELETE FROM public.notification WHERE "to" IS NULL;

ALTER TABLE public.notification
    ALTER COLUMN "to" SET NOT NULL,
    DROP COLUMN mailed_at,
    DROP COLUMN read_at,
    DROP COLUMN user_id;

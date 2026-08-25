ALTER TABLE public.notification
    ADD COLUMN user_id   uuid REFERENCES public."user"(user_id) ON DELETE CASCADE,
    ADD COLUMN read_at   timestamptz,
    ADD COLUMN mailed_at timestamptz,
    ALTER COLUMN "to" DROP NOT NULL;

CREATE INDEX notification_user_created_idx
    ON public.notification (user_id, created_at DESC);
CREATE INDEX notification_key_user_created_idx
    ON public.notification (key, user_id, created_at DESC);

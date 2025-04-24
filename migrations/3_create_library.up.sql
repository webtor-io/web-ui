-- 1. Create table "library" with composite primary key
CREATE TABLE public.library (
    user_id uuid NOT NULL,
    resource_id text NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,

    CONSTRAINT library_pk PRIMARY KEY (user_id, resource_id),
    CONSTRAINT library_user_fk FOREIGN KEY (user_id)
        REFERENCES public."user"(user_id)
        ON DELETE CASCADE
);
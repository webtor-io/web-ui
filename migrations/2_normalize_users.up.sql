-- 1. Create user table
CREATE TABLE public."user" (
   user_id uuid DEFAULT uuid_generate_v4() PRIMARY KEY,
   email text NOT NULL UNIQUE,
   password text,
   created_at timestamptz DEFAULT now() NOT NULL,
   updated_at timestamptz DEFAULT now() NOT NULL
);

-- 2. Create trigger to auto-update updated_at
CREATE OR REPLACE FUNCTION public.update_user_updated_at()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
   NEW.updated_at = now();
RETURN NEW;
END;
$$;

CREATE TRIGGER update_user_updated_at
    BEFORE UPDATE ON public."user"
    FOR EACH ROW
    EXECUTE FUNCTION public.update_user_updated_at();

-- 3. Add user_id column to embed_domain
ALTER TABLE public.embed_domain
    ADD COLUMN user_id uuid;

-- 4. Insert distinct emails into user table with empty passwords
INSERT INTO public."user" (email, password)
SELECT DISTINCT email, '' FROM public.embed_domain;

-- 5. Populate user_id in embed_domain based on matching email
UPDATE public.embed_domain ed
SET user_id = u.user_id
    FROM public."user" u
WHERE ed.email = u.email;

-- 6. Enforce NOT NULL and foreign key constraint
ALTER TABLE public.embed_domain
    ALTER COLUMN user_id SET NOT NULL;

ALTER TABLE public.embed_domain
    ADD CONSTRAINT embed_domain_user_fk FOREIGN KEY (user_id)
        REFERENCES public."user"(user_id)
        ON DELETE CASCADE;

-- 7. Drop old email column from embed_domain
ALTER TABLE public.embed_domain
DROP COLUMN email;
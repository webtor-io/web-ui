-- 1. Re-add email column to embed_domain
ALTER TABLE public.embed_domain
    ADD COLUMN email text;

-- 2. Restore email values from user table
UPDATE public.embed_domain ed
SET email = u.email
    FROM public."user" u
WHERE ed.user_id = u.user_id;

-- 3. Drop foreign key and user_id column
ALTER TABLE public.embed_domain
DROP CONSTRAINT embed_domain_user_fk;

ALTER TABLE public.embed_domain
DROP COLUMN user_id;

-- 4. Drop user table and related trigger/function
DROP TRIGGER IF EXISTS update_user_updated_at ON public."user";
DROP FUNCTION IF EXISTS public.update_user_updated_at();
DROP TABLE IF EXISTS public."user";
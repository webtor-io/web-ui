DROP INDEX IF EXISTS public.speedtest_result_session_id_idx;
ALTER TABLE public.speedtest_result DROP COLUMN IF EXISTS session_id;

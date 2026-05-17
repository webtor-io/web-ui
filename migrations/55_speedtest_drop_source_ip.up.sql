DROP INDEX IF EXISTS public.speedtest_result_source_ip_idx;

ALTER TABLE public.speedtest_result
	DROP COLUMN IF EXISTS source_ip;

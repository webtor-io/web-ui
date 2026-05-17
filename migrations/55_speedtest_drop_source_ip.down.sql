ALTER TABLE public.speedtest_result
	ADD COLUMN source_ip inet;

CREATE INDEX IF NOT EXISTS speedtest_result_source_ip_idx
	ON public.speedtest_result (source_ip);

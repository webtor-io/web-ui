DELETE FROM public.speedtest_result WHERE dest_ip IS NULL OR dest_ip = '';

ALTER TABLE public.speedtest_result
	ALTER COLUMN source_ip TYPE inet USING source_ip::inet,
	ALTER COLUMN dest_ip TYPE inet USING dest_ip::inet,
	ALTER COLUMN dest_ip SET NOT NULL;

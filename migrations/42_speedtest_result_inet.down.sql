ALTER TABLE public.speedtest_result
	ALTER COLUMN source_ip TYPE text USING source_ip::text,
	ALTER COLUMN dest_ip DROP NOT NULL,
	ALTER COLUMN dest_ip TYPE text USING dest_ip::text;

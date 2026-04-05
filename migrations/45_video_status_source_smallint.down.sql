ALTER TABLE public.episode_status
	ALTER COLUMN source TYPE text USING CASE source
		WHEN 1 THEN 'manual'
		WHEN 2 THEN 'auto_90pct'
		WHEN 3 THEN 'auto_all_episodes'
	END;

ALTER TABLE public.series_status
	ALTER COLUMN source TYPE text USING CASE source
		WHEN 1 THEN 'manual'
		WHEN 2 THEN 'auto_90pct'
		WHEN 3 THEN 'auto_all_episodes'
	END;

ALTER TABLE public.movie_status
	ALTER COLUMN source TYPE text USING CASE source
		WHEN 1 THEN 'manual'
		WHEN 2 THEN 'auto_90pct'
		WHEN 3 THEN 'auto_all_episodes'
	END;

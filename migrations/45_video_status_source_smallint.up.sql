-- Convert movie_status / series_status / episode_status .source from text to
-- smallint, mapping the three known values to a compact numeric enum that the
-- Go side mirrors as models.UserVideoSource:
--   1 = manual            (strongest signal, explicit user intent)
--   2 = auto_90pct        (derived from the 90% playback threshold)
--   3 = auto_all_episodes (derived when every known episode is watched)
--
-- Any row with an unexpected textual value would become NULL under the CASE,
-- which would violate the NOT NULL constraint and abort the migration —
-- intentional, since the source set is closed and an unknown value signals a
-- bug we want to catch, not silently coerce.

ALTER TABLE public.movie_status
	ALTER COLUMN source TYPE smallint USING CASE source
		WHEN 'manual'            THEN 1
		WHEN 'auto_90pct'        THEN 2
		WHEN 'auto_all_episodes' THEN 3
	END;

ALTER TABLE public.series_status
	ALTER COLUMN source TYPE smallint USING CASE source
		WHEN 'manual'            THEN 1
		WHEN 'auto_90pct'        THEN 2
		WHEN 'auto_all_episodes' THEN 3
	END;

ALTER TABLE public.episode_status
	ALTER COLUMN source TYPE smallint USING CASE source
		WHEN 'manual'            THEN 1
		WHEN 'auto_90pct'        THEN 2
		WHEN 'auto_all_episodes' THEN 3
	END;

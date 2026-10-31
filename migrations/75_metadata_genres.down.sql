ALTER TABLE public.series_metadata
	DROP COLUMN IF EXISTS genres;

ALTER TABLE public.movie_metadata
	DROP COLUMN IF EXISTS genres;

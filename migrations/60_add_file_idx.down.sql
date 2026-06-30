ALTER TABLE public.episode
	DROP COLUMN IF EXISTS file_idx,
	DROP COLUMN IF EXISTS file_size;

ALTER TABLE public.movie
	DROP COLUMN IF EXISTS file_idx,
	DROP COLUMN IF EXISTS file_size;

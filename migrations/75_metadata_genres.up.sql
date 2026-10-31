-- A title's genres, as slugs of the fixed vocabulary in services/genre
-- (docs/genres.md), derived from TMDB's genre ids.
--
--   NULL  not derived yet: the row was written by a mapper with no genre
--         data (OMDB, Kinopoisk) or before this column existed. The genres
--         backfill in `enrich popular` fills it from tmdb.info.
--   '{}'  derived, none: TMDB lists no genres, or has no row for the title.
--
-- No default on purpose: an insert that leaves the column out means "not
-- derived", and a nullable column without a default is added without a
-- table rewrite.
ALTER TABLE public.movie_metadata
	ADD COLUMN genres text[];

ALTER TABLE public.series_metadata
	ADD COLUMN genres text[];

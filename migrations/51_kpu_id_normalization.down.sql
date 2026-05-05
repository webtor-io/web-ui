-- Reverse migration 51: restore the KPU-advertised imdbId as video_id for
-- records whose poster came from Kinopoisk Unofficial.
--
-- WARNING: rolling back also requires reverting the matching code change
-- in services/enrich/kinopoisk_unofficial.go (makeVideoMetadata). Without
-- the code revert the mapper continues to emit kp{id} for newly-enriched
-- rows, so the rollback is partial.

UPDATE public.movie_metadata mm
   SET video_id = k.imdb_id
  FROM kinopoisk_unofficial.info k
 WHERE mm.poster_url LIKE '%kinopoiskapiunofficial%'
   AND mm.video_id = 'kp' || k.kp_id::text
   AND k.imdb_id IS NOT NULL
   AND k.imdb_id <> '';

UPDATE public.series_metadata sm
   SET video_id = k.imdb_id
  FROM kinopoisk_unofficial.info k
 WHERE sm.poster_url LIKE '%kinopoiskapiunofficial%'
   AND sm.video_id = 'kp' || k.kp_id::text
   AND k.imdb_id IS NOT NULL
   AND k.imdb_id <> '';

UPDATE public.movie_status ms
   SET video_id = k.imdb_id
  FROM kinopoisk_unofficial.info k, public.movie_metadata mm
 WHERE ms.video_id = 'kp' || k.kp_id::text
   AND mm.video_id = k.imdb_id
   AND mm.poster_url LIKE '%kinopoiskapiunofficial%'
   AND k.imdb_id IS NOT NULL
   AND k.imdb_id <> '';

UPDATE public.series_status ss
   SET video_id = k.imdb_id
  FROM kinopoisk_unofficial.info k, public.series_metadata sm
 WHERE ss.video_id = 'kp' || k.kp_id::text
   AND sm.video_id = k.imdb_id
   AND sm.poster_url LIKE '%kinopoiskapiunofficial%'
   AND k.imdb_id IS NOT NULL
   AND k.imdb_id <> '';

UPDATE public.episode_status es
   SET video_id = k.imdb_id
  FROM kinopoisk_unofficial.info k, public.series_metadata sm
 WHERE es.video_id = 'kp' || k.kp_id::text
   AND sm.video_id = k.imdb_id
   AND sm.poster_url LIKE '%kinopoiskapiunofficial%'
   AND k.imdb_id IS NOT NULL
   AND k.imdb_id <> '';

UPDATE public.episode_metadata em
   SET video_id = k.imdb_id
  FROM kinopoisk_unofficial.info k, public.series_metadata sm
 WHERE em.video_id = 'kp' || k.kp_id::text
   AND sm.video_id = k.imdb_id
   AND sm.poster_url LIKE '%kinopoiskapiunofficial%'
   AND k.imdb_id IS NOT NULL
   AND k.imdb_id <> '';

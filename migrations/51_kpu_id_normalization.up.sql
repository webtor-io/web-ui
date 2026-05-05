-- Normalise metadata records sourced from Kinopoisk Unofficial (KPU) so
-- they are identified by our internal kp{kp_id}, never by the imdbId
-- KPU advertises in their response.
--
-- Why: KPU's imdbId field is unreliable. It can point to an unrelated film
-- with the same English title — e.g. their kp=306084 ("Теория большого
-- взрыва") returns imdbId=tt1147717, which is actually a 2007 short film,
-- not the CBS sitcom (tt0898266). When we save such an imdbId as our
-- video_id, the poster handler later cannot resolve it through TMDB and
-- the card 500s. Keeping the identifier and the poster from the same
-- source is an invariant.
--
-- Going forward services/enrich/kinopoisk_unofficial.go always emits
-- kp{kp_id}; this migration repairs historical rows.

-- Cascade-update referencing tables first while their old video_ids are
-- still resolvable. For each affected (poster_url-from-KPU, video_id-tt*)
-- row we look up MIN(kp_id) — disambiguates the rare case where KPU has
-- two of their own records pointing to the same imdbId (e.g. duplicates
-- in their dataset).

WITH affected_movies AS (
    SELECT mm.video_id AS old_id,
           'kp' || (SELECT MIN(kp_id) FROM kinopoisk_unofficial.info k WHERE k.imdb_id = mm.video_id)::text AS new_id
      FROM public.movie_metadata mm
     WHERE mm.poster_url LIKE '%kinopoiskapiunofficial%'
       AND mm.video_id LIKE 'tt%'
       AND EXISTS (SELECT 1 FROM kinopoisk_unofficial.info k WHERE k.imdb_id = mm.video_id)
), affected_series AS (
    SELECT sm.video_id AS old_id,
           'kp' || (SELECT MIN(kp_id) FROM kinopoisk_unofficial.info k WHERE k.imdb_id = sm.video_id)::text AS new_id
      FROM public.series_metadata sm
     WHERE sm.poster_url LIKE '%kinopoiskapiunofficial%'
       AND sm.video_id LIKE 'tt%'
       AND EXISTS (SELECT 1 FROM kinopoisk_unofficial.info k WHERE k.imdb_id = sm.video_id)
)
UPDATE public.movie_status ms
   SET video_id = a.new_id
  FROM affected_movies a
 WHERE ms.video_id = a.old_id;

WITH affected_series AS (
    SELECT sm.video_id AS old_id,
           'kp' || (SELECT MIN(kp_id) FROM kinopoisk_unofficial.info k WHERE k.imdb_id = sm.video_id)::text AS new_id
      FROM public.series_metadata sm
     WHERE sm.poster_url LIKE '%kinopoiskapiunofficial%'
       AND sm.video_id LIKE 'tt%'
       AND EXISTS (SELECT 1 FROM kinopoisk_unofficial.info k WHERE k.imdb_id = sm.video_id)
)
UPDATE public.series_status ss
   SET video_id = a.new_id
  FROM affected_series a
 WHERE ss.video_id = a.old_id;

WITH affected_series AS (
    SELECT sm.video_id AS old_id,
           'kp' || (SELECT MIN(kp_id) FROM kinopoisk_unofficial.info k WHERE k.imdb_id = sm.video_id)::text AS new_id
      FROM public.series_metadata sm
     WHERE sm.poster_url LIKE '%kinopoiskapiunofficial%'
       AND sm.video_id LIKE 'tt%'
       AND EXISTS (SELECT 1 FROM kinopoisk_unofficial.info k WHERE k.imdb_id = sm.video_id)
)
UPDATE public.episode_status es
   SET video_id = a.new_id
  FROM affected_series a
 WHERE es.video_id = a.old_id;

WITH affected_series AS (
    SELECT sm.video_id AS old_id,
           'kp' || (SELECT MIN(kp_id) FROM kinopoisk_unofficial.info k WHERE k.imdb_id = sm.video_id)::text AS new_id
      FROM public.series_metadata sm
     WHERE sm.poster_url LIKE '%kinopoiskapiunofficial%'
       AND sm.video_id LIKE 'tt%'
       AND EXISTS (SELECT 1 FROM kinopoisk_unofficial.info k WHERE k.imdb_id = sm.video_id)
)
UPDATE public.episode_metadata em
   SET video_id = a.new_id
  FROM affected_series a
 WHERE em.video_id = a.old_id;

-- Now flip the metadata tables themselves. Done last so cascading updates
-- above could still match by old_id.

UPDATE public.series_metadata sm
   SET video_id = 'kp' || k.kp_id::text
  FROM (
    SELECT imdb_id, MIN(kp_id) AS kp_id
      FROM kinopoisk_unofficial.info
     WHERE imdb_id IS NOT NULL AND imdb_id <> ''
     GROUP BY imdb_id
  ) AS k
 WHERE sm.poster_url LIKE '%kinopoiskapiunofficial%'
   AND sm.video_id LIKE 'tt%'
   AND sm.video_id = k.imdb_id;

UPDATE public.movie_metadata mm
   SET video_id = 'kp' || k.kp_id::text
  FROM (
    SELECT imdb_id, MIN(kp_id) AS kp_id
      FROM kinopoisk_unofficial.info
     WHERE imdb_id IS NOT NULL AND imdb_id <> ''
     GROUP BY imdb_id
  ) AS k
 WHERE mm.poster_url LIKE '%kinopoiskapiunofficial%'
   AND mm.video_id LIKE 'tt%'
   AND mm.video_id = k.imdb_id;

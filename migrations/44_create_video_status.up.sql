-- Movies: per-user watched flag for a movie identified by IMDB video_id.
CREATE TABLE public.movie_status (
	user_id		uuid		NOT NULL,
	video_id	text		NOT NULL,
	watched		boolean		NOT NULL DEFAULT true,
	rating		smallint,
	source		text		NOT NULL,
	watched_at	timestamptz,
	created_at	timestamptz	NOT NULL DEFAULT now(),
	updated_at	timestamptz	NOT NULL DEFAULT now(),

	CONSTRAINT movie_status_pk PRIMARY KEY (user_id, video_id),
	CONSTRAINT movie_status_user_fk FOREIGN KEY (user_id)
		REFERENCES public."user"(user_id)
		ON DELETE CASCADE
);

CREATE INDEX movie_status_user_updated_idx
	ON public.movie_status (user_id, updated_at DESC);

CREATE TRIGGER update_movie_status_updated_at
	BEFORE UPDATE ON public.movie_status
	FOR EACH ROW EXECUTE FUNCTION update_updated_at();


-- Series: per-user "I watched the whole series" declaration.
-- Either set manually or derived automatically when all known episodes are watched.
CREATE TABLE public.series_status (
	user_id		uuid		NOT NULL,
	video_id	text		NOT NULL,
	watched		boolean		NOT NULL DEFAULT true,
	rating		smallint,
	source		text		NOT NULL,
	watched_at	timestamptz,
	created_at	timestamptz	NOT NULL DEFAULT now(),
	updated_at	timestamptz	NOT NULL DEFAULT now(),

	CONSTRAINT series_status_pk PRIMARY KEY (user_id, video_id),
	CONSTRAINT series_status_user_fk FOREIGN KEY (user_id)
		REFERENCES public."user"(user_id)
		ON DELETE CASCADE
);

CREATE INDEX series_status_user_updated_idx
	ON public.series_status (user_id, updated_at DESC);

CREATE TRIGGER update_series_status_updated_at
	BEFORE UPDATE ON public.series_status
	FOR EACH ROW EXECUTE FUNCTION update_updated_at();


-- Episodes: per-user watched flag for a specific episode of a series,
-- keyed on series IMDB video_id + season + episode (IMDB-scoped, not torrent-scoped).
CREATE TABLE public.episode_status (
	user_id		uuid		NOT NULL,
	video_id	text		NOT NULL,
	season		smallint	NOT NULL,
	episode		smallint	NOT NULL,
	watched		boolean		NOT NULL DEFAULT true,
	rating		smallint,
	source		text		NOT NULL,
	watched_at	timestamptz,
	created_at	timestamptz	NOT NULL DEFAULT now(),
	updated_at	timestamptz	NOT NULL DEFAULT now(),

	CONSTRAINT episode_status_pk PRIMARY KEY (user_id, video_id, season, episode),
	CONSTRAINT episode_status_user_fk FOREIGN KEY (user_id)
		REFERENCES public."user"(user_id)
		ON DELETE CASCADE
);

CREATE INDEX episode_status_user_updated_idx
	ON public.episode_status (user_id, updated_at DESC);

CREATE INDEX episode_status_user_video_idx
	ON public.episode_status (user_id, video_id);

CREATE TRIGGER update_episode_status_updated_at
	BEFORE UPDATE ON public.episode_status
	FOR EACH ROW EXECUTE FUNCTION update_updated_at();


-- Backfill: pull existing watch_history.watched=true rows into the new status tables
-- via JOINs to movie/episode + their metadata (to obtain IMDB video_id).
-- Rows whose enrichment did not resolve a video_id are intentionally skipped;
-- they remain valid in watch_history but are not part of the IMDB-keyed profile.

-- 1. Movies
INSERT INTO public.movie_status
	(user_id, video_id, watched, source, watched_at, created_at, updated_at)
SELECT DISTINCT
	wh.user_id,
	mm.video_id,
	true,
	'auto_90pct',
	wh.updated_at,
	wh.updated_at,
	wh.updated_at
FROM public.watch_history wh
JOIN public.movie m ON m.resource_id = wh.resource_id
JOIN public.movie_metadata mm ON mm.movie_metadata_id = m.movie_metadata_id
WHERE wh.watched = true
	AND mm.video_id IS NOT NULL
ON CONFLICT (user_id, video_id) DO NOTHING;

-- 2. Episodes
INSERT INTO public.episode_status
	(user_id, video_id, season, episode, watched, source, watched_at, created_at, updated_at)
SELECT DISTINCT
	wh.user_id,
	sm.video_id,
	e.season,
	e.episode,
	true,
	'auto_90pct',
	wh.updated_at,
	wh.updated_at,
	wh.updated_at
FROM public.watch_history wh
JOIN public.episode e ON e.resource_id = wh.resource_id AND e.path = wh.path
JOIN public.series s ON s.series_id = e.series_id
JOIN public.series_metadata sm ON sm.series_metadata_id = s.series_metadata_id
WHERE wh.watched = true
	AND sm.video_id IS NOT NULL
	AND e.season IS NOT NULL
	AND e.episode IS NOT NULL
ON CONFLICT (user_id, video_id, season, episode) DO NOTHING;

-- 3. Series-level auto-mark: for every (user_id, video_id) where all episodes
-- known in episode_metadata are marked watched, insert a series-level row.
INSERT INTO public.series_status
	(user_id, video_id, watched, source, watched_at, created_at, updated_at)
SELECT
	es.user_id,
	es.video_id,
	true,
	'auto_all_episodes',
	MAX(es.watched_at),
	MAX(es.watched_at),
	MAX(es.watched_at)
FROM public.episode_status es
JOIN (
	SELECT video_id, COUNT(*) AS total
	FROM public.episode_metadata
	GROUP BY video_id
) em ON em.video_id = es.video_id
WHERE es.watched = true
GROUP BY es.user_id, es.video_id, em.total
HAVING COUNT(*) = em.total
ON CONFLICT (user_id, video_id) DO NOTHING;

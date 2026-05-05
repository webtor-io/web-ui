-- Movies: per-user "I want to watch this" entries identified by IMDB video_id.
-- Symmetric to movie_status (migration 44). No FK on movie_metadata so a user
-- can add an item we haven't enriched yet — the metadata cache is filled in
-- lazily by the handler via Enricher.LookupByVideoID.
--
-- source records the entry point that produced the row, used for analytics
-- and as a future signal for the recommendation engine (see UserVideoSource
-- in models/movie_status.go for the analogous enum on watched rows). Stored
-- as text rather than a smallint enum because the discover entry points
-- (catalog grid, AI recommendations, search results, streamy modal) are
-- product-level surfaces that may be renamed; a free-form column keeps
-- iteration cheap without a schema migration per rename.
CREATE TABLE public.movie_watchlist (
	user_id		uuid		NOT NULL,
	video_id	text		NOT NULL,
	source		text		NOT NULL,
	created_at	timestamptz	NOT NULL DEFAULT now(),

	CONSTRAINT movie_watchlist_pk PRIMARY KEY (user_id, video_id),
	CONSTRAINT movie_watchlist_user_fk FOREIGN KEY (user_id)
		REFERENCES public."user"(user_id)
		ON DELETE CASCADE
);

CREATE INDEX movie_watchlist_user_created_idx
	ON public.movie_watchlist (user_id, created_at DESC);


CREATE TABLE public.series_watchlist (
	user_id		uuid		NOT NULL,
	video_id	text		NOT NULL,
	source		text		NOT NULL,
	created_at	timestamptz	NOT NULL DEFAULT now(),

	CONSTRAINT series_watchlist_pk PRIMARY KEY (user_id, video_id),
	CONSTRAINT series_watchlist_user_fk FOREIGN KEY (user_id)
		REFERENCES public."user"(user_id)
		ON DELETE CASCADE
);

CREATE INDEX series_watchlist_user_created_idx
	ON public.series_watchlist (user_id, created_at DESC);

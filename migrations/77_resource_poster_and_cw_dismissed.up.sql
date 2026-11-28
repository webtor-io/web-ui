-- Poster the user saw in Discover when they opened a title, keyed by the
-- torrent that was started. The poster resolver prefers it over every
-- metadata-provider poster, so Continue Watching shows the same artwork as
-- the catalog even on an instance with no TMDB/OMDB key. First writer wins:
-- the row is never replaced, so one user cannot overwrite another's poster.
CREATE TABLE public.resource_poster (
	resource_id	text		NOT NULL,
	poster_url	text		NOT NULL,
	created_at	timestamptz	NOT NULL DEFAULT now(),

	CONSTRAINT resource_poster_pk PRIMARY KEY (resource_id)
);

-- Titles a user removed from Continue Watching with the x button. A row hides
-- the resource only until the user watches it again (dismissed_at is compared
-- with the watch_history row's updated_at), so no watch progress is deleted.
CREATE TABLE public.continue_watching_dismissed (
	user_id		uuid		NOT NULL,
	resource_id	text		NOT NULL,
	dismissed_at	timestamptz	NOT NULL DEFAULT now(),

	CONSTRAINT continue_watching_dismissed_pk PRIMARY KEY (user_id, resource_id),
	CONSTRAINT continue_watching_dismissed_user_fk FOREIGN KEY (user_id)
		REFERENCES public."user"(user_id)
		ON DELETE CASCADE
);

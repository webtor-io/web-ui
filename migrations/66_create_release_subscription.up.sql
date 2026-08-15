CREATE TABLE public.release_subscription (
	release_subscription_id uuid DEFAULT uuid_generate_v4() NOT NULL,
	user_id uuid NOT NULL,
	kind varchar(16) NOT NULL,
	video_id text NOT NULL,
	season int2,
	title text,
	poster_url text,
	lang varchar(8) DEFAULT 'en' NOT NULL,
	source varchar(32) DEFAULT 'other' NOT NULL,
	enabled bool DEFAULT true NOT NULL,
	state varchar(24) DEFAULT 'pending_baseline' NOT NULL,
	last_checked_at timestamptz,
	next_check_at timestamptz DEFAULT now() NOT NULL,
	last_notified_at timestamptz,
	created_at timestamptz DEFAULT now() NOT NULL,
	updated_at timestamptz DEFAULT now() NOT NULL,
	CONSTRAINT release_subscription_pk PRIMARY KEY (release_subscription_id),
	CONSTRAINT release_subscription_user_fk FOREIGN KEY (user_id)
		REFERENCES public."user" (user_id) ON DELETE CASCADE
);

-- A movie subscription has no season, so the uniqueness of (user, content)
-- cannot be a plain constraint: NULL never equals NULL and the same film
-- could be subscribed to twice. coalesce folds the movie case onto a value
-- no real season takes.
CREATE UNIQUE INDEX release_subscription_content_unique
	ON public.release_subscription (user_id, kind, video_id, coalesce(season, (-1)::int2));

-- The poller's working set: everything due, oldest first. Rows that are
-- switched off or finished never appear in it, so they cost nothing.
CREATE INDEX release_subscription_due_idx
	ON public.release_subscription (next_check_at)
	WHERE enabled = true AND state <> 'completed';

CREATE INDEX release_subscription_user_idx
	ON public.release_subscription (user_id, created_at DESC);

create trigger update_updated_at before
update
    on
    public.release_subscription for each row execute function update_updated_at();

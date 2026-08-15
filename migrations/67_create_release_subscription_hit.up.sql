-- One row per infohash a subscription has ever seen. The primary key is what
-- enforces "never tell the user about the same release twice": the poller
-- inserts everything it finds with ON CONFLICT DO NOTHING, and the mailer
-- only ever reads rows whose notified_at is still null.
CREATE TABLE public.release_subscription_hit (
	release_subscription_id uuid NOT NULL,
	infohash varchar(40) NOT NULL,
	name text,
	size int8,
	source_name text,
	season int2,
	episode int2,
	is_baseline bool DEFAULT false NOT NULL,
	first_seen_at timestamptz DEFAULT now() NOT NULL,
	notified_at timestamptz,
	CONSTRAINT release_subscription_hit_pk PRIMARY KEY (release_subscription_id, infohash),
	CONSTRAINT release_subscription_hit_subscription_fk FOREIGN KEY (release_subscription_id)
		REFERENCES public.release_subscription (release_subscription_id) ON DELETE CASCADE
);

-- The mailer's query: what is still owed to this subscription.
CREATE INDEX release_subscription_hit_pending_idx
	ON public.release_subscription_hit (release_subscription_id)
	WHERE notified_at IS NULL;

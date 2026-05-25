-- Per-user preferences that don't fit existing tables. Single row
-- per user, FK CASCADE so deletion of the user wipes their settings.
--
-- Typed columns (not JSONB) because the v1 setting set is small,
-- discrete, and we may want filter-by-flag queries (e.g. "how many
-- users opted into adult content"). Switch to JSONB if we ever start
-- accumulating long-tail boolean flags that nothing queries.
--
-- Defaults are the SAFE / OFF state — a missing row reads as
-- "everything default". Models load nil-safely.
CREATE TABLE public.user_settings (
	user_id		uuid		NOT NULL REFERENCES public."user"(user_id) ON DELETE CASCADE,
	-- show_adult: when true the unified poster endpoint serves the
	-- /raw/ variant (no Gaussian blur) and the 18+ overlay-badge
	-- isn't rendered on cards. Default false — accidental-view
	-- protection is the safer baseline.
	show_adult	boolean		NOT NULL DEFAULT false,
	created_at	timestamptz	NOT NULL DEFAULT now(),
	updated_at	timestamptz	NOT NULL DEFAULT now(),

	CONSTRAINT user_settings_pk PRIMARY KEY (user_id)
);

CREATE TRIGGER update_user_settings_updated_at
	BEFORE UPDATE ON public.user_settings
	FOR EACH ROW EXECUTE FUNCTION update_updated_at();

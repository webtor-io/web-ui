-- Per-subscription overrides of the two stream preferences the account
-- already carries. Both are snapshots taken when the subscription is
-- created, not references: changing the profile later must not silently
-- change what an existing subscription reports.
--
-- NULL means "no preference of its own" — for the language that is "any",
-- and for the resolutions an empty/absent list means the same.
ALTER TABLE public.release_subscription
	ADD COLUMN preferred_resolutions jsonb,
	ADD COLUMN preferred_language varchar(8);

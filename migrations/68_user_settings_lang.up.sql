-- The account had no language of its own: it lived in the URL prefix and a
-- cookie, neither of which reaches a cron job. Notifications are sent from
-- one, so the language has to be stored.
--
-- Nullable on purpose — NULL means "never observed", which is a different
-- thing from "chose English", and lets the sender fall back to the language
-- captured when a subscription was created.
ALTER TABLE public.user_settings
	ADD COLUMN lang varchar(8);

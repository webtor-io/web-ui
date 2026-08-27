-- Adopt the pre-feed journal rows into their owner's feed.
--
-- Migration 70 added notification.user_id and deliberately did not backfill
-- it: the table was reasoned about as a mail-dedupe log rather than a history
-- worth showing, so the feed was meant to start empty. Against a real database
-- that reasoning does not hold. The rows ARE the notifications a person was
-- sent, and someone who received them opens the page and finds nothing.
--
-- Ownership is recovered from the address the letter went to. Only an exact
-- match against the identity email is adopted; a row addressed to someone who
-- no longer has an account stays ownerless and so stays out of every feed.
UPDATE public.notification AS n
   SET user_id = u.user_id,
       -- Read, not unread. These letters were delivered when they were new,
       -- so putting years of them in the navbar badge would announce a
       -- backlog that no action of the reader's created. They belong in the
       -- history, not in the count.
       read_at = COALESCE(n.read_at, n.created_at)
  FROM public."user" AS u
 WHERE n.user_id IS NULL
   AND n."to" IS NOT NULL
   AND n."to" = u.email;

-- mailed_at is deliberately left NULL.
--
-- It would be easy to set it to created_at on the grounds that these rows
-- only ever existed because a letter went out. That is true wherever SMTP was
-- configured -- but it is exactly the claim this column was added to stop the
-- table making on faith. An instance running the old code with no mail server
-- still wrote these rows, because the mailer returned success when it was
-- unconfigured. Stamping them here would re-tell that lie in the one place
-- built to end it.
--
-- The cost of leaving it NULL is bounded and one-off: a notification whose
-- key repeats within 24 hours of this migration can be mailed a second time.
-- Anything older than that window is unaffected.

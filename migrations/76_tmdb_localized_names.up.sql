-- Person names in the row's language, keyed by TMDB person id (a decimal
-- string): directors, creators and the top of the cast, from the same
-- localized details call that fills title and plot. See docs/discover.md,
-- «Credits (TMDB)».
--
--   NULL  not fetched yet: the row was stored before this column existed.
--         Reading it queues a background refresh of the row.
--   '{}'  fetched, nothing to store.
--
-- No default on purpose: a nullable column without a default is added
-- without a table rewrite.
ALTER TABLE tmdb.localized
	ADD COLUMN names jsonb;

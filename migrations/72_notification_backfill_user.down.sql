-- No-op, on purpose.
--
-- Undoing the backfill would mean clearing user_id on exactly the rows this
-- migration set it on, and nothing on the row records that. A row adopted
-- here is indistinguishable from one written with an owner afterwards, so any
-- reversal would either miss rows or take genuine feed entries away from
-- their owners.
--
-- Rolling back further is unaffected: 70's down drops the column outright.
SELECT 1;

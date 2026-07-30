-- The onboarding checklist asks "does this user have anything in Vault?" on
-- every page render that shows the navbar counter:
--   EXISTS (SELECT 1 FROM vault.pledge WHERE user_id = ?)
-- vault.pledge had no index with user_id as the leading column — migration 26
-- creates only pledge_pk (pledge_id) and migration 33 adds
-- UNIQUE (resource_id, user_id), whose leading column is the wrong one — so the
-- predicate could not seek. Worst case is precisely the cohort the checklist
-- targets: a user with no pledges cannot short-circuit and scans the lot.
CREATE INDEX IF NOT EXISTS idx_pledge_user_id
	ON vault.pledge (user_id);

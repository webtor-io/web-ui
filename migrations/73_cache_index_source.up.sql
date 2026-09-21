-- Where an index entry came from, because the two kinds are taken back
-- differently.
--
--   1 probe   somebody asked the backend at play time ("is it cached?") and it
--           said yes. For Webtor that answer covers the seeder AND the Vault
--           and does not say which. True as of the question; forgotten by age.
--   2 seeder  the seeder reported the file complete on its disk
--           (resource.cached). Taken back by resource.uncached from the seeder
--           or its disk cleaner; the age limit is only the backstop for a node
--           that died without saying anything.
--
-- One shared row would let a cleaner's "gone from my disk" erase the knowledge
-- that the same file is in the Vault. With the source in the key, an event can
-- only remove what an event wrote.
-- A number, not a word: the set is closed and lives in the code
-- (models.CacheSource). Numbered from 1 -- go-pg leaves a zero value out of an
-- insert, and 0 would silently become the default.
ALTER TABLE public.cache_index
	ADD COLUMN source smallint NOT NULL DEFAULT 1;

ALTER TABLE public.cache_index
	DROP CONSTRAINT cache_index_resource_file_idx_backend_unique;

ALTER TABLE public.cache_index
	ADD CONSTRAINT cache_index_resource_file_idx_backend_source_unique
		UNIQUE (resource_id, file_idx, backend_type, source);

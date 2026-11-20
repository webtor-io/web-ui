-- No default on purpose: a nullable column without a default is added without a table rewrite.
--
-- torrent_resource.updated_at is when an upload last changed its size or
-- file count; NULL for torrents and for an upload whose contents never
-- changed. Movie and series detection older than it is redone. No
-- update_updated_at trigger on purpose: a trigger would move the stamp on
-- any UPDATE of the row, a rename too, and redo the detection for nothing.
--
-- upload_reserve is the space an upload ID holds against its owner's quota.
-- Rows go away only with the account (ON DELETE CASCADE).
ALTER TABLE torrent_resource ADD COLUMN updated_at timestamptz;
CREATE TABLE upload_reserve (
  user_id        uuid        NOT NULL REFERENCES public."user"(user_id) ON DELETE CASCADE,
  resource_id    text        NOT NULL,
  reserved_bytes bigint      NOT NULL DEFAULT 0,
  write_until    timestamptz,
  gone_at        timestamptz,
  PRIMARY KEY (user_id, resource_id)
);

UPDATE vault.resource SET name = '' WHERE name IS NULL;
ALTER TABLE vault.resource ALTER COLUMN name SET NOT NULL;

-- Add the new column before the data-copy migration runs.
ALTER TABLE migration_version_test ADD COLUMN normalized_name TEXT;

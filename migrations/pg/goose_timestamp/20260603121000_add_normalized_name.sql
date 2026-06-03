-- +goose Up
-- Add the new column before the data-copy migration runs.
ALTER TABLE migration_version_test ADD COLUMN normalized_name TEXT;

-- +goose Down
-- Remove the new column when the migration is rolled back.
ALTER TABLE migration_version_test DROP COLUMN normalized_name;

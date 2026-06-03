-- +goose Up
-- Copy existing data into the new column after tests seed old-shape rows.
UPDATE migration_version_test
SET normalized_name = UPPER(legacy_name)
WHERE normalized_name IS NULL;

-- +goose Down
-- Clear copied data when the migration is rolled back.
UPDATE migration_version_test SET normalized_name = NULL;

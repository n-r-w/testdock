-- Remove the new column when the migration is rolled back.
ALTER TABLE migration_version_test DROP COLUMN normalized_name;

-- Clear copied data when the migration is rolled back.
UPDATE migration_version_test SET normalized_name = NULL;

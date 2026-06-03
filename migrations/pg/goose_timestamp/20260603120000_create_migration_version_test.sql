-- +goose Up
-- Create the old schema that production data already uses.
CREATE TABLE migration_version_test (
  id SERIAL PRIMARY KEY,
  legacy_name TEXT NOT NULL
);

-- +goose Down
-- Remove the old schema when the migration is rolled back.
DROP TABLE migration_version_test;

-- Create the old schema that production data already uses.
CREATE TABLE migration_version_test (
  id SERIAL PRIMARY KEY,
  legacy_name TEXT NOT NULL
);

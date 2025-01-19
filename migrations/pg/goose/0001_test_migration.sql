-- +goose Up
CREATE TABLE test_table (
  id SERIAL PRIMARY KEY,
  name TEXT NOT NULL
);

INSERT INTO test_table (name) VALUES ('test');

-- +goose Down
DROP TABLE test_table;

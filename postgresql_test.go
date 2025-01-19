package testdock

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"
)

const testPostgresImage = "17.2"

func Test_PgxGooseDB(t *testing.T) {
	t.Parallel()

	db := GetPgxPool(t,
		"postgres://postgres:secret@127.0.0.1:5432/postgres?sslmode=disable",
		WithMigrations("migrations/pg/goose", GooseMigrateFactory(goose.DialectPostgres, "pgx")),
		WithDockerImage(testPostgresImage),
	)

	testPgxHelper(t, db)
}

func Test_PgxGomigrateDB(t *testing.T) {
	t.Parallel()

	db := GetPgxPool(t,
		"postgres://postgres:secret@127.0.0.1:5432/postgres?sslmode=disable",
		WithMigrations("migrations/pg/gomigrate", GolangMigrateFactory),
		WithDockerImage(testPostgresImage),
	)

	testPgxHelper(t, db)
}

func Test_LibPGDB(t *testing.T) {
	t.Parallel()

	db := GetPqConn(t,
		"postgres://postgres:secret@127.0.0.1:5432/postgres?sslmode=disable",
		WithMigrations("migrations/pg/goose", GooseMigrateFactory(goose.DialectPostgres, "postgres")),
		WithDockerImage(testPostgresImage),
	)

	testSQLHelper(t, db)
}

func testPgxHelper(t *testing.T, db *pgxpool.Pool) {
	t.Helper()

	rows, err := db.Query(context.Background(), "SELECT name FROM test_table")
	if err != nil {
		t.Fatalf("error: %s", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("error: %s", err)
		}
		if name != "test" { //nolint:goconst // ok
			t.Fatalf("expected 'test', got '%s'", name)
		}
	}
}

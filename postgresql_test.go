package testdock

import (
	"context"
	"testing"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5/pgxpool"
)

const testPostgresImage = "17.2"

func Test_PgxGooseDB(t *testing.T) {
	t.Parallel()

	db, informer := GetPgxPool(t,
		DefaultPostgresDSN,
		WithMigrations("migrations/pg/goose", GooseMigrateFactoryPGX),
		WithDockerImage(testPostgresImage),
	)

	checkInformer(t, DefaultPostgresDSN, informer)

	testPgxHelper(t, db)
}

func Test_PgxGomigrateDB(t *testing.T) {
	t.Parallel()

	db, informer := GetPgxPool(t,
		DefaultPostgresDSN,
		WithMigrations("migrations/pg/gomigrate", GolangMigrateFactory),
		WithDockerImage(testPostgresImage),
		WithMode(RunModeDocker), // force run in docker
	)

	checkInformer(t, DefaultPostgresDSN, informer)

	testPgxHelper(t, db)
}

func Test_LibPGDB(t *testing.T) {
	t.Parallel()

	db, _ := GetPqConn(t,
		DefaultPostgresDSN,
		WithMigrations("migrations/pg/goose", GooseMigrateFactoryPQ),
		WithDockerImage(testPostgresImage),
	)

	testSQLHelper(t, db)
}

func testPgxHelper(t *testing.T, db *pgxpool.Pool) {
	t.Helper()

	var rows []struct {
		Name string `db:"name"`
	}
	if err := pgxscan.Select(context.Background(), db, &rows, "SELECT name FROM test_table"); err != nil {
		t.Fatalf("error querying test_table: %s", err)
	}

	if len(rows) == 0 {
		t.Fatal("no rows returned from test_table")
	}

	if rows[0].Name != "test" { //nolint:goconst // ok
		t.Fatalf("expected 'test', got '%s'", rows[0].Name)
	}
}

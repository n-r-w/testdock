package pglive

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func Test_InitGooseDB(t *testing.T) {
	t.Parallel()

	db := GetPool(t, "./migrations/goose",
		WithForceDockerMode(),
		WithMigratorFactory(GooseMigratorFactory))

	testHelper(t, db)
}

func Test_InitGomigrateDB(t *testing.T) {
	t.Parallel()

	db := GetPool(t, "./migrations/gomigrate",
		WithForceDockerMode(),
		WithMigratorFactory(GolangMigrateFactory))

	testHelper(t, db)
}

func testHelper(t *testing.T, db *pgxpool.Pool) {
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
		if name != "test" {
			t.Fatalf("expected 'test', got '%s'", name)
		}
	}
}

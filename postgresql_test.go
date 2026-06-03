package testdock

import (
	"context"
	"testing"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const (
	testPostgresImage                    = "17.2"
	testTimestampMigrationInitialVersion = int64(20260603120000)
	testTimestampMigrationColumnVersion  = int64(20260603121000)
)

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

	ctx := context.Background()
	db, _ := GetPqConn(ctx, t,
		DefaultPostgresDSN,
		WithMigrations("migrations/pg/goose", GooseMigrateFactoryPQ),
		WithDockerImage(testPostgresImage),
	)

	testSQLHelper(t, db)
}

// TestWithMigrationsToVersionAppliesTimestampPrefixBoundaryForGoose verifies that goose treats
// the target version as the numeric timestamp prefix from the migration file name.
func TestWithMigrationsToVersionAppliesTimestampPrefixBoundaryForGoose(t *testing.T) {
	t.Parallel()

	runTimestampMigrationBoundaryTest(t, "migrations/pg/goose_timestamp", GooseMigrateFactoryPGX)
}

// TestWithMigrationsToVersionAppliesTimestampPrefixBoundaryForGolangMigrate verifies that
// golang-migrate treats the target version as the numeric timestamp prefix from the file name.
func TestWithMigrationsToVersionAppliesTimestampPrefixBoundaryForGolangMigrate(t *testing.T) {
	t.Parallel()

	runTimestampMigrationBoundaryTest(t, "migrations/pg/gomigrate_timestamp", GolangMigrateFactory)
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

	if rows[0].Name != "test" {
		t.Fatalf("expected 'test', got '%s'", rows[0].Name)
	}
}

// runTimestampMigrationBoundaryTest verifies the full migration-copy scenario.
func runTimestampMigrationBoundaryTest(t *testing.T, migrationsDir string, migrateFactory MigrateFactory) {
	t.Helper()

	ctx := context.Background()
	db, informer := GetPgxPool(t,
		DefaultPostgresDSN,
		WithMigrationsToVersion(migrationsDir, migrateFactory, testTimestampMigrationInitialVersion),
		WithDockerImage(testPostgresImage),
		WithMode(RunModeDocker),
	)

	// The first migration creates only the old schema, so tests can seed old-shape data.
	assertNormalizedNameColumn(t, ctx, db, false)

	_, err := db.Exec(ctx, "INSERT INTO migration_version_test (legacy_name) VALUES ($1)", "alice")
	require.NoError(t, err)

	// Applying migrations to the column version must expose the new column without copying data yet.
	ApplyMigrationsToVersion(t, informer.DSN(), migrationsDir, migrateFactory, testTimestampMigrationColumnVersion)
	assertNormalizedNameColumn(t, ctx, db, true)
	assertNormalizedNameIsNull(t, ctx, db, "alice", true)

	// Applying the remaining migrations must copy data from the old column into the new column.
	ApplyMigrations(t, informer.DSN(), migrationsDir, migrateFactory)

	var normalizedName string
	err = db.QueryRow(ctx,
		"SELECT normalized_name FROM migration_version_test WHERE legacy_name = $1",
		"alice",
	).Scan(&normalizedName)
	require.NoError(t, err)
	require.Equal(t, "ALICE", normalizedName)
}

// assertNormalizedNameColumn checks the schema boundary between old and new migrations.
func assertNormalizedNameColumn(t *testing.T, ctx context.Context, db *pgxpool.Pool, wantExists bool) {
	t.Helper()

	var exists bool
	err := db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_name = 'migration_version_test'
				AND column_name = 'normalized_name'
		)
	`).Scan(&exists)
	require.NoError(t, err)
	require.Equal(t, wantExists, exists)
}

// assertNormalizedNameIsNull checks whether the data-copy migration has already run.
func assertNormalizedNameIsNull(t *testing.T, ctx context.Context, db *pgxpool.Pool, legacyName string, wantNull bool) {
	t.Helper()

	var isNull bool
	err := db.QueryRow(ctx,
		"SELECT normalized_name IS NULL FROM migration_version_test WHERE legacy_name = $1",
		legacyName,
	).Scan(&isNull)
	require.NoError(t, err)
	require.Equal(t, wantNull, isNull)
}

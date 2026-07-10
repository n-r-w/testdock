package testdock

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/n-r-w/ctxlog"
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

// TestPostgresTemplateClonesPreparedDatabaseInParallel verifies that one prepared source supplies
// isolated migrated databases to parallel tests without repeating source preparation.
func TestPostgresTemplateClonesPreparedDatabaseInParallel(t *testing.T) {
	var migrationFactoryCalls atomic.Int32

	// Count migrator construction so cloned databases cannot silently rerun the migration pipeline.
	migrateFactory := func(
		tb testing.TB,
		dsn string,
		migrationsDir string,
		logger ctxlog.ILogger,
	) (Migrator, error) {
		migrationFactoryCalls.Add(1)
		return GooseMigrateFactoryPGX(tb, dsn, migrationsDir, logger)
	}

	// Prepare one migrated source database and add state that every clone must inherit.
	template := NewPostgresTemplate(
		t,
		DefaultPostgresDSN,
		WithPostgresTemplateOptions(
			WithMigrations("migrations/pg/goose", migrateFactory),
			WithDockerImage(testPostgresImage),
			WithMode(RunModeDocker),
		),
		WithPostgresTemplateSetup(func(tb testing.TB, pool *pgxpool.Pool, _ Informer) {
			_, err := pool.Exec(tb.Context(), "INSERT INTO test_table (name) VALUES ($1)", "template")
			require.NoError(tb, err)
		}),
	)

	const cloneCount = 4
	cloneNames := make(chan string, cloneCount)

	// Create and mutate clones concurrently to prove both parallel creation and database isolation.
	t.Run("parallel clones", func(t *testing.T) {
		for range cloneCount {
			t.Run("clone", func(t *testing.T) {
				t.Parallel()

				pool, informer := template.GetPgxPool(t)
				cloneNames <- informer.DatabaseName()

				var initialRows int
				err := pool.QueryRow(t.Context(), "SELECT count(*) FROM test_table").Scan(&initialRows)
				require.NoError(t, err)
				require.Equal(t, 2, initialRows)

				_, err = pool.Exec(t.Context(), "INSERT INTO test_table (name) VALUES ($1)", t.Name())
				require.NoError(t, err)

				var rowsAfterMutation int
				err = pool.QueryRow(t.Context(), "SELECT count(*) FROM test_table").Scan(&rowsAfterMutation)
				require.NoError(t, err)
				require.Equal(t, 3, rowsAfterMutation)
			})
		}
	})

	// Every child must receive a distinct database while sharing the single source preparation.
	close(cloneNames)
	uniqueNames := make(map[string]struct{}, cloneCount)
	for databaseName := range cloneNames {
		uniqueNames[databaseName] = struct{}{}
	}
	require.Len(t, uniqueNames, cloneCount)
	require.EqualValues(t, 1, migrationFactoryCalls.Load())
}

// TestPostgresTemplateKeepsSourceUntilChildrenFinish verifies that child cleanup drops only its
// clone and leaves the parent-owned source available for later children.
func TestPostgresTemplateKeepsSourceUntilChildrenFinish(t *testing.T) {
	// Prepare a parent-owned source without migrations because this test checks only lifecycle.
	template := NewPostgresTemplate(
		t,
		DefaultPostgresDSN,
		WithPostgresTemplateOptions(
			WithDockerImage(testPostgresImage),
			WithMode(RunModeDocker),
		),
	)

	var completedCloneName string
	t.Run("completed clone", func(t *testing.T) {
		_, informer := template.GetPgxPool(t)
		completedCloneName = informer.DatabaseName()
	})

	// A later child can still clone the source and observe that the completed child's database is gone.
	verificationPool, _ := template.GetPgxPool(t)
	var completedCloneExists bool
	err := verificationPool.QueryRow(
		t.Context(),
		"SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)",
		completedCloneName,
	).Scan(&completedCloneExists)
	require.NoError(t, err)
	require.False(t, completedCloneExists)
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

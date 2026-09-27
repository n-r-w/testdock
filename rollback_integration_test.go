package testdock

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rollbackBackend describes one built-in migrator and its version bookkeeping.
type rollbackBackend struct {
	name       string         // Test name and fixture format selector.
	factory    MigrateFactory // Built-in migration factory.
	versionSQL string         // Query returning the currently applied migration version.
}

// rollbackBackends covers both PostgreSQL Goose drivers and golang-migrate.
func rollbackBackends() []rollbackBackend {
	return []rollbackBackend{
		{name: "goose-pgx", factory: GooseMigrateFactoryPGX,
			versionSQL: "SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version WHERE is_applied"},
		{name: "goose-pq", factory: GooseMigrateFactoryPQ,
			versionSQL: "SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version WHERE is_applied"},
		{name: "golang-migrate", factory: GolangMigrateFactory,
			versionSQL: "SELECT COALESCE(MAX(version), 0) FROM schema_migrations"},
	}
}

// TestRollbackRoundTrip checks targeted rollback, full rollback, no-op boundaries, and reapplication.
func TestRollbackRoundTrip(t *testing.T) {
	t.Parallel()
	for _, backend := range rollbackBackends() {
		for _, initial := range []int64{1, 20260603120000} {
			// Two independent databases per backend exercise concurrent rollback without global state.
			t.Run(fmt.Sprintf("%s/%d", backend.name, initial), func(t *testing.T) {
				t.Parallel()
				// Arrange two applied migrations with either sequential or timestamp prefixes.
				dir := writeRollbackMigrations(t, backend.name, initial, false)
				pool, info := GetPgxPool(t, DefaultPostgresDSN, WithDockerImage(testPostgresImage))
				applyRollbackFixtures(t, info.DSN(), dir, backend.factory)
				assertRollbackSchema(t, pool, backend, initial+1, true, true)

				// Act and assert: retain the first migration and never migrate forward to a higher target.
				RollbackMigrationsToVersion(t, info.DSN(), dir, backend.factory, initial)
				assertRollbackSchema(t, pool, backend, initial, true, false)
				RollbackMigrationsToVersion(t, info.DSN(), dir, backend.factory, initial)
				RollbackMigrationsToVersion(t, info.DSN(), dir, backend.factory, initial+1)
				assertRollbackSchema(t, pool, backend, initial, true, false)

				// Act and assert: full rollback removes all application objects and can be repeated.
				RollbackMigrations(t, info.DSN(), dir, backend.factory)
				assertRollbackSchema(t, pool, backend, 0, false, false)
				RollbackMigrations(t, info.DSN(), dir, backend.factory)
				RollbackMigrationsToVersion(t, info.DSN(), dir, backend.factory, initial)
				assertRollbackSchema(t, pool, backend, 0, false, false)

				// Act and assert: the existing apply helper restores the complete schema.
				applyRollbackFixtures(t, info.DSN(), dir, backend.factory)
				assertRollbackSchema(t, pool, backend, initial+1, true, true)
				RollbackMigrations(t, info.DSN(), dir, backend.factory)
				assertRollbackConnectionsClosed(t, pool, info.DatabaseName())
			})
		}
	}
}

// TestRollbackSQLFailureClosesConnections checks both helpers and database cleanup after invalid down SQL.
func TestRollbackSQLFailureClosesConnections(t *testing.T) {
	t.Parallel()
	for _, backend := range rollbackBackends() {
		for _, targeted := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/targeted=%t", backend.name, targeted), func(t *testing.T) {
				t.Parallel()
				// Arrange a parent-owned Docker server and an observer for child database deletion.
				observer, server := GetPgxPool(t, DefaultPostgresDSN,
					WithDockerImage(testPostgresImage), WithMode(RunModeDocker))
				var databaseName string
				t.Run("database", func(t *testing.T) {
					dir := writeRollbackMigrations(t, backend.name, 1, true)
					// External mode drops the child database; Docker mode only releases the shared container.
					pool, info := GetPgxPool(t, server.DSN(), WithMode(RunModeExternal))
					databaseName = info.DatabaseName()
					applyRollbackFixtures(t, info.DSN(), dir, backend.factory)
					tb := &fatalRecorder{TB: t, message: ""}
					done := make(chan struct{})

					// Act through the public helper and capture its expected test failure.
					go func() {
						defer close(done)
						if targeted {
							RollbackMigrationsToVersion(tb, info.DSN(), dir, backend.factory, 1)
						} else {
							RollbackMigrations(tb, info.DSN(), dir, backend.factory)
						}
					}()
					<-done

					// Assert PostgreSQL's cause survives and no migration connection remains.
					require.Contains(t, tb.message, "syntax error")
					assertRollbackConnectionsClosed(t, pool, databaseName)
				})

				// Assert normal testdock cleanup dropped the database even after failed rollback.
				var exists bool
				require.NoError(t, observer.QueryRow(t.Context(),
					"SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", databaseName).Scan(&exists))
				assert.False(t, exists)
			})
		}
	}
}

// writeRollbackMigrations creates matching fixtures with an optional failure in the second down script.
func writeRollbackMigrations(t *testing.T, backend string, initial int64, broken bool) string {
	t.Helper()
	dir := t.TempDir()
	for index, scripts := range [][2]string{
		{"CREATE TABLE rollback_test (id INTEGER PRIMARY KEY);", "DROP TABLE rollback_test;"},
		{"ALTER TABLE rollback_test ADD COLUMN label TEXT;", "ALTER TABLE rollback_test DROP COLUMN label;"},
	} {
		if broken && index == 1 {
			scripts[1] = "INVALID DOWN SQL;"
		}
		prefix := fmt.Sprintf("%015d_rollback", initial+int64(index))
		if backend == "golang-migrate" {
			require.NoError(t, os.WriteFile(filepath.Join(dir, prefix+".up.sql"), []byte(scripts[0]), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(dir, prefix+".down.sql"), []byte(scripts[1]), 0o600))
		} else {
			sql := "-- +goose Up\n" + scripts[0] + "\n-- +goose Down\n" + scripts[1] + "\n"
			require.NoError(t, os.WriteFile(filepath.Join(dir, prefix+".sql"), []byte(sql), 0o600))
		}
	}
	return dir
}

// applyRollbackFixtures applies all forward migrations for a rollback test.
func applyRollbackFixtures(t *testing.T, dsn, dir string, factory MigrateFactory) {
	t.Helper()
	ApplyMigrations(t, dsn, dir, factory)
}

// assertRollbackSchema checks version metadata and every application schema change in the fixture.
func assertRollbackSchema(t *testing.T, pool *pgxpool.Pool, backend rollbackBackend, version int64, table, column bool) {
	t.Helper()
	var actualVersion int64
	require.NoError(t, pool.QueryRow(t.Context(), backend.versionSQL).Scan(&actualVersion))
	assert.Equal(t, version, actualVersion)
	var actualTable, actualColumn bool
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT to_regclass('public.rollback_test') IS NOT NULL").Scan(&actualTable))
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'rollback_test' AND column_name = 'label')`).Scan(&actualColumn))
	assert.Equal(t, table, actualTable)
	assert.Equal(t, column, actualColumn)
}

// assertRollbackConnectionsClosed distinguishes the caller's pool from leaked migration connections.
func assertRollbackConnectionsClosed(t *testing.T, pool *pgxpool.Pool, databaseName string) {
	t.Helper()
	var otherConnections int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT COUNT(*) FROM pg_stat_activity
		WHERE datname = $1 AND pid <> pg_backend_pid()`, databaseName).Scan(&otherConnections))
	assert.Zero(t, otherConnections)
}

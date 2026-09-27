package testdock

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
	"github.com/golang-migrate/migrate/v4/source"
	"github.com/lib/pq"
	"github.com/n-r-w/ctxlog"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingMigrationDatabase simulates a version-read failure and an independent close failure.
type failingMigrationDatabase struct {
	database.Driver       // Unused operations remain outside this test's execution path.
	versionErr      error // Migration failure reported by Version.
	closeErr        error // Database close failure.
	closed          bool  // Records synchronous resource release.
}

var _ database.Driver = (*failingMigrationDatabase)(nil)

// Lock allows the migration engine to reach the controlled version-read failure.
func (*failingMigrationDatabase) Lock() error { return nil }

// Unlock completes failure handling without replacing the original error.
func (*failingMigrationDatabase) Unlock() error { return nil }

// Version supplies the controlled migration failure.
func (d *failingMigrationDatabase) Version() (int, bool, error) { return -1, false, d.versionErr }

// Close records resource release and supplies an independent database error.
func (d *failingMigrationDatabase) Close() error { d.closed = true; return d.closeErr }

// failingMigrationSource simulates a source close failure without reading migration files.
type failingMigrationSource struct {
	source.Driver       // Source reads are unnecessary when the version query fails.
	closeErr      error // Source close failure.
	closed        bool  // Records synchronous resource release.
}

var _ source.Driver = (*failingMigrationSource)(nil)

// Close records source release and returns the controlled error.
func (s *failingMigrationSource) Close() error { s.closed = true; return s.closeErr }

// TestRollbackGolangMigrateJoinsCloseErrors checks all three error causes and immediate closure.
func TestRollbackGolangMigrateJoinsCloseErrors(t *testing.T) {
	t.Parallel()
	for _, targeted := range []bool{false, true} {
		for _, migrationErr := range []error{nil, errors.New("version query failed")} {
			t.Run(fmt.Sprintf("targeted=%t/error=%v", targeted, migrationErr), func(t *testing.T) {
				t.Parallel()
				// Arrange an empty database or a version-read failure, plus two close failures.
				sourceErr := errors.New("source close failed")
				databaseErr := errors.New("database close failed")
				db := &failingMigrationDatabase{Driver: nil, versionErr: migrationErr, closeErr: databaseErr, closed: false}
				src := &failingMigrationSource{Driver: nil, closeErr: sourceErr, closed: false}
				engine, err := migrate.NewWithInstance("fixture", src, "fixture", db)
				require.NoError(t, err)
				factory := func(testing.TB, string, string, ctxlog.ILogger) (Migrator, error) {
					return &golangMigrateMigrator{m: engine}, nil
				}

				// Act through the shared runner used by public rollback helpers.
				err = runMigrations(t.Context(), t, "dsn", "dir", factory, ctxlog.Must(ctxlog.WithTesting(t)),
					migrationRunConfig{retryTimeout: DefaultRetryTimeout, totalRetryDuration: DefaultTotalRetryDuration,
						rollback: true, hasTargetVersion: targeted, targetVersion: 1})

				// Assert every error remains identifiable, even after an otherwise successful no-op.
				if migrationErr != nil {
					require.ErrorIs(t, err, migrationErr)
				}
				require.ErrorIs(t, err, sourceErr)
				require.ErrorIs(t, err, databaseErr)
				assert.True(t, src.closed)
				assert.True(t, db.closed)
			})
		}
	}
}

// closeErrorConnector wraps a real database connector to inject a failure after connection closure.
type closeErrorConnector struct {
	driver.Connector       // Underlying PostgreSQL connector.
	closeErr         error // Error added after real resource release.
}

var _ driver.Connector = closeErrorConnector{}

// Connect returns a real connection whose Close method reports the controlled error.
func (c closeErrorConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return closeErrorConnection{Conn: conn, closeErr: c.closeErr}, nil
}

// closeErrorConnection retains standard driver operations and overrides only Close.
type closeErrorConnection struct {
	driver.Conn       // Real connection used by Goose for SQL execution.
	closeErr    error // Error returned after the real connection closes.
}

var _ driver.Conn = closeErrorConnection{}

// Close releases the real connection and preserves both close errors.
func (c closeErrorConnection) Close() error { return errors.Join(c.Conn.Close(), c.closeErr) }

// TestRollbackGooseJoinsCloseError verifies Goose preserves SQL and close failures for both helpers.
func TestRollbackGooseJoinsCloseError(t *testing.T) {
	t.Parallel()
	for _, targeted := range []bool{false, true} {
		for _, broken := range []bool{false, true} {
			t.Run(fmt.Sprintf("targeted=%t/broken=%t", targeted, broken), func(t *testing.T) {
				t.Parallel()
				// Arrange real migration SQL and a connector that fails only after closing.
				dir := writeRollbackMigrations(t, "goose-pq", 1, broken)
				pool, info := GetPgxPool(t, DefaultPostgresDSN, WithDockerImage(testPostgresImage))
				applyRollbackFixtures(t, info.DSN(), dir, GooseMigrateFactoryPQ)
				closeErr := errors.New("injected connection close failure")
				connector, err := pq.NewConnector(info.DSN())
				require.NoError(t, err)
				db := sql.OpenDB(closeErrorConnector{Connector: connector, closeErr: closeErr})
				provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS(dir))
				require.NoError(t, err)
				factory := func(testing.TB, string, string, ctxlog.ILogger) (Migrator, error) {
					return &gooseMigrator{db: db, p: provider}, nil
				}

				// Act with or without a migration error before the close error.
				err = runMigrations(t.Context(), t, info.DSN(), dir, factory, ctxlog.Must(ctxlog.WithTesting(t)),
					migrationRunConfig{retryTimeout: DefaultRetryTimeout, totalRetryDuration: DefaultTotalRetryDuration,
						rollback: true, hasTargetVersion: targeted, targetVersion: 1})

				// Assert close errors are never discarded, even when the migration itself succeeds.
				require.ErrorIs(t, err, closeErr)
				if broken {
					require.ErrorContains(t, err, "syntax error")
				}
				assertRollbackConnectionsClosed(t, pool, info.DatabaseName())
			})
		}
	}
}

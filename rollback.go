package testdock

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/n-r-w/ctxlog"
)

// RollbackMigrations rolls back every applied migration in an existing temporary database.
// The helper fails tb on invalid input or migration errors. Built-in migrators close
// their resources before returning and preserve both migration and close errors.
// Custom factories must return a migrator that implements RollbackMigrator.
func RollbackMigrations(tb testing.TB, dsn, migrationsDir string, migrateFactory MigrateFactory) {
	tb.Helper()

	logger := ctxlog.Must(ctxlog.WithTesting(tb))
	err := runMigrations(tb.Context(), tb, dsn, migrationsDir, migrateFactory, logger, migrationRunConfig{
		retryTimeout:       DefaultRetryTimeout,
		totalRetryDuration: DefaultTotalRetryDuration,
		targetVersion:      0,
		hasTargetVersion:   false,
		rollback:           true,
	})
	if err != nil {
		tb.Fatalf("cannot roll back migrations: %v", err)
	}
}

// RollbackMigrationsToVersion rolls back migrations newer than the positive target version.
// The version is the numeric file prefix before "_", including timestamp prefixes.
// A target at or above the current version leaves the database unchanged.
// Resource ownership and failure behavior match RollbackMigrations.
func RollbackMigrationsToVersion(
	tb testing.TB, dsn, migrationsDir string, migrateFactory MigrateFactory, version int64,
) {
	tb.Helper()

	logger := ctxlog.Must(ctxlog.WithTesting(tb))
	err := runMigrations(tb.Context(), tb, dsn, migrationsDir, migrateFactory, logger, migrationRunConfig{
		retryTimeout:       DefaultRetryTimeout,
		totalRetryDuration: DefaultTotalRetryDuration,
		targetVersion:      version,
		hasTargetVersion:   true,
		rollback:           true,
	})
	if err != nil {
		tb.Fatalf("cannot roll back migrations to version: %v", err)
	}
}

// migrateDown selects one rollback operation without retrying migration SQL.
func migrateDown(ctx context.Context, migrator Migrator, config migrationRunConfig) error {
	rollback, ok := migrator.(RollbackMigrator)
	if !ok {
		return errors.New("RollbackMigrations and RollbackMigrationsToVersion require " +
			"migrator to implement RollbackMigrator")
	}

	if config.hasTargetVersion {
		if err := rollback.DownTo(ctx, config.targetVersion); err != nil {
			return fmt.Errorf("down migrations to version: %w", err)
		}
		return nil
	}

	if err := rollback.Down(ctx); err != nil {
		return fmt.Errorf("down migrations: %w", err)
	}
	return nil
}

// Down rolls back every Goose migration and closes the per-call provider synchronously.
func (m *gooseMigrator) Down(ctx context.Context) error {
	_, err := m.p.DownTo(ctx, 0)
	return errors.Join(err, m.p.Close())
}

// DownTo retains Goose migrations at or below the target and preserves close errors.
func (m *gooseMigrator) DownTo(ctx context.Context, version int64) error {
	if err := validateMigrationVersion(version); err != nil {
		return errors.Join(err, m.p.Close())
	}

	_, err := m.p.DownTo(ctx, version)
	return errors.Join(err, m.p.Close())
}

// Down rolls back every golang-migrate migration and closes both underlying drivers.
func (m *golangMigrateMigrator) Down(_ context.Context) error {
	return m.finishRollback(m.m.Down())
}

// DownTo prevents golang-migrate's bidirectional Migrate operation from applying new migrations.
func (m *golangMigrateMigrator) DownTo(_ context.Context, version int64) error {
	target, err := migrationVersionToUint(version)
	if err != nil {
		return m.finishRollback(err)
	}

	current, dirty, err := m.m.Version()
	switch {
	case err != nil && !errors.Is(err, migrate.ErrNilVersion):
		return m.finishRollback(err)
	case dirty:
		return m.finishRollback(fmt.Errorf("dirty migration version %d", current))
	case errors.Is(err, migrate.ErrNilVersion), current <= target:
		return m.finishRollback(nil)
	default:
		return m.finishRollback(m.m.Migrate(target))
	}
}

// finishRollback treats an already rolled-back database as success and retains every close failure.
func (m *golangMigrateMigrator) finishRollback(err error) error {
	if errors.Is(err, migrate.ErrNoChange) {
		err = nil
	}

	sourceErr, databaseErr := m.m.Close()
	if sourceErr != nil {
		sourceErr = fmt.Errorf("close migration source: %w", sourceErr)
	}
	if databaseErr != nil {
		databaseErr = fmt.Errorf("close migration database: %w", databaseErr)
	}
	return errors.Join(err, sourceErr, databaseErr)
}

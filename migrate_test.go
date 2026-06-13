package testdock

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	testInvalidMigrationVersion = int64(0)
	testValidMigrationVersion   = int64(1)
)

// TestWithMigrationsToVersionRejectsInvalidVersion verifies early validation before migrations run.
func TestWithMigrationsToVersionRejectsInvalidVersion(t *testing.T) {
	t.Parallel()

	db := &testDB{
		t:                         nil,
		logger:                    nil,
		databaseName:              "",
		url:                       nil,
		dsnNoPass:                 "",
		driver:                    "pgx",
		mode:                      RunModeExternal,
		dsn:                       DefaultPostgresDSN,
		retryTimeout:              DefaultRetryTimeout,
		totalRetryDuration:        DefaultTotalRetryDuration,
		closeTimeout:              defaultCloseTimeout,
		migrationsDir:             "",
		migrationTargetVersion:    0,
		hasMigrationTargetVersion: false,
		unsetProxyEnv:             false,
		migrateFactory:            nil,
		prepareCleanUp:            nil,
		connectDatabase:           "",
		connectDatabaseOverride:   false,
		dockerPort:                0,
		dockerRepository:          "",
		dockerImage:               "",
		dockerSocketEndpoint:      "",
		dockerEnv:                 nil,
	}

	err := db.prepareOptions("pgx", []Option{
		WithMigrationsToVersion("migrations/pg/goose", GooseMigrateFactoryPGX, testInvalidMigrationVersion),
	})
	require.ErrorContains(t, err, "migration target version")
	require.ErrorContains(t, err, "migration version must be greater than 0")
}

// TestMigrateUpToVersionRequiresVersionedMigrator verifies the custom factory contract.
func TestMigrateUpToVersionRequiresVersionedMigrator(t *testing.T) {
	t.Parallel()

	err := migrateUpToVersion(context.Background(), upOnlyMigrator{}, testValidMigrationVersion)
	require.ErrorContains(t, err, "WithMigrationsToVersion")
	require.ErrorContains(t, err, "VersionedMigrator")
}

// upOnlyMigrator simulates a custom factory result that supports full migration only.
type upOnlyMigrator struct{}

// Up implements the existing Migrator contract without version-limited migration support.
func (upOnlyMigrator) Up(_ context.Context) error {
	return nil
}

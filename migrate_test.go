package testdock

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/n-r-w/ctxlog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testInvalidMigrationVersion = int64(0)
	testValidMigrationVersion   = int64(1)
	testTooManyClientsSQLState  = "53300"
)

// TestRetryMigrationConnectionRetriesTooManyClients verifies that temporary PostgreSQL saturation waits for capacity.
func TestRetryMigrationConnectionRetriesTooManyClients(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		// ARRANGE: Fail two connection attempts with the PostgreSQL capacity error.
		attempts := 0
		logger := ctxlog.Must(ctxlog.WithTesting(t))

		// ACT: Retry until the third connection attempt succeeds.
		err := retryMigrationConnection(t.Context(), logger, time.Second, 10*time.Second, func() error {
			attempts++
			if attempts < 3 {
				return fmt.Errorf("connect migration database: %w", testSQLStateError{state: testTooManyClientsSQLState})
			}
			return nil
		})

		// ASSERT: Only connection preparation is repeated.
		require.NoError(t, err)
		assert.Equal(t, 3, attempts)
	})
}

// TestRetryMigrationConnectionReturnsOtherErrors verifies that unrelated connection failures are not retried.
func TestRetryMigrationConnectionReturnsOtherErrors(t *testing.T) {
	t.Parallel()

	// ARRANGE: Use an error without the PostgreSQL capacity SQLSTATE.
	expectedErr := errors.New("authentication failed")
	attempts := 0
	logger := ctxlog.Must(ctxlog.WithTesting(t))

	// ACT: Execute connection preparation with the non-retryable error.
	err := retryMigrationConnection(t.Context(), logger, time.Second, 10*time.Second, func() error {
		attempts++
		return expectedErr
	})

	// ASSERT: The original error is returned after one attempt.
	require.ErrorIs(t, err, expectedErr)
	assert.Equal(t, 1, attempts)
}

// testSQLStateError represents a driver error that exposes its PostgreSQL SQLSTATE.
type testSQLStateError struct {
	state string
}

// Error implements error for connection retry tests.
func (e testSQLStateError) Error() string {
	return "SQLSTATE " + e.state
}

// SQLState exposes the PostgreSQL error code through the driver-independent contract.
func (e testSQLStateError) SQLState() string {
	return e.state
}

// TestWithMigrationsToVersionRejectsInvalidVersion verifies early validation before migrations run.
func TestWithMigrationsToVersionRejectsInvalidVersion(t *testing.T) {
	t.Parallel()

	db := &testDB{
		t:                         nil,
		logger:                    nil,
		databaseName:              "",
		databaseTemplate:          "",
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

package testdock

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/n-r-w/ctxlog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rollbackRecorder records the selected operation and simulates a migration failure.
type rollbackRecorder struct {
	upCalls   int     // Number of unexpected forward operations.
	downCalls int     // Number of full rollback operations.
	versions  []int64 // Requested rollback targets.
	err       error   // Error returned by rollback operations.
}

var (
	_ Migrator         = (*rollbackRecorder)(nil)
	_ RollbackMigrator = (*rollbackRecorder)(nil)
)

// Up records a forward migration so rollback tests detect accidental application.
func (m *rollbackRecorder) Up(context.Context) error { m.upCalls++; return nil }

// Down records one full rollback attempt.
func (m *rollbackRecorder) Down(context.Context) error { m.downCalls++; return m.err }

// DownTo records the requested target version.
func (m *rollbackRecorder) DownTo(_ context.Context, version int64) error {
	m.versions = append(m.versions, version)
	return m.err
}

// TestRollbackDispatch verifies full and targeted rollback without forward execution or SQL retries.
func TestRollbackDispatch(t *testing.T) {
	t.Parallel()
	for _, targeted := range []bool{false, true} {
		for _, operationErr := range []error{nil, testSQLStateError{state: testTooManyClientsSQLState}} {
			t.Run(fmt.Sprintf("targeted=%t/error=%v", targeted, operationErr), func(t *testing.T) {
				t.Parallel()
				// Arrange a custom rollback migrator and a positive timestamp target.
				m := &rollbackRecorder{upCalls: 0, downCalls: 0, versions: nil, err: operationErr}
				factory := func(testing.TB, string, string, ctxlog.ILogger) (Migrator, error) { return m, nil }
				config := migrationRunConfig{retryTimeout: time.Millisecond, totalRetryDuration: time.Second,
					targetVersion: 20260603120000, hasTargetVersion: targeted, rollback: true}

				// Act through the same runner used by the public helpers.
				err := runMigrations(t.Context(), t, "dsn", "dir", factory, ctxlog.Must(ctxlog.WithTesting(t)), config)

				// Assert one down operation, preserved errors, and no forward migration.
				require.ErrorIs(t, err, operationErr)
				assert.Zero(t, m.upCalls)
				if targeted {
					assert.Equal(t, []int64{config.targetVersion}, m.versions)
					assert.Zero(t, m.downCalls)
				} else {
					assert.Equal(t, 1, m.downCalls)
					assert.Empty(t, m.versions)
				}
			})
		}
	}
}

// TestRollbackRejectsUnsupportedMigrator verifies compatibility with existing up-only factories.
func TestRollbackRejectsUnsupportedMigrator(t *testing.T) {
	t.Parallel()
	// Arrange an existing custom factory that has no rollback methods.
	factory := func(testing.TB, string, string, ctxlog.ILogger) (Migrator, error) { return upOnlyMigrator{}, nil }
	config := migrationRunConfig{retryTimeout: DefaultRetryTimeout, totalRetryDuration: DefaultTotalRetryDuration,
		targetVersion: 0, hasTargetVersion: false, rollback: true}

	// Act without changing the original Migrator interface.
	err := runMigrations(t.Context(), t, "dsn", "dir", factory, ctxlog.Must(ctxlog.WithTesting(t)), config)

	// Assert a diagnostic that names the optional rollback contract.
	require.ErrorContains(t, err, "RollbackMigrator")
}

// fatalRecorder captures a test failure while preserving Fatalf's goroutine termination behavior.
type fatalRecorder struct {
	testing.TB        // Real test supplies context, logging, and helper methods.
	message    string // Failure text written before the goroutine exits.
}

// Fatalf records the diagnostic and terminates the helper's goroutine like testing.TB.
func (tb *fatalRecorder) Fatalf(format string, args ...any) {
	tb.message = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

// TestRollbackHelpersFailTest verifies public diagnostics for invalid inputs and operation failures.
func TestRollbackHelpersFailTest(t *testing.T) {
	t.Parallel()
	operationErr := errors.New("down SQL failed; close connection failed")
	for _, tc := range []struct {
		name     string         // Scenario name.
		dsn      string         // Database address supplied to the helper.
		dir      string         // Migration directory supplied to the helper.
		version  int64          // Positive target or an invalid boundary.
		targeted bool           // Selects the versioned helper.
		factory  MigrateFactory // Factory result under test.
		want     string         // Required diagnostic fragment.
	}{
		{name: "empty DSN", dsn: "", dir: "dir", version: 0, targeted: false, factory: nil, want: "dsn is empty"},
		{name: "empty directory", dsn: "dsn", dir: "", version: 0, targeted: false, factory: nil, want: "migrationsDir is empty"},
		{name: "nil factory", dsn: "dsn", dir: "dir", version: 0, targeted: false, factory: nil, want: "migrateFactory is nil"},
		{name: "zero version", dsn: "dsn", dir: "dir", version: 0, targeted: true, factory: recorderFactory(nil), want: "greater than 0"},
		{name: "negative version", dsn: "dsn", dir: "dir", targeted: true, version: -1,
			factory: recorderFactory(nil), want: "greater than 0"},
		{name: "factory failure", dsn: "dsn", dir: "dir", version: 0, targeted: false, factory: func(testing.TB, string, string, ctxlog.ILogger) (Migrator, error) {
			return nil, errors.New("factory failed")
		}, want: "factory failed"},
		{name: "nil migrator", dsn: "dsn", dir: "dir", version: 0, targeted: false, factory: func(testing.TB, string, string, ctxlog.ILogger) (Migrator, error) {
			return nil, nil //nolint:nilnil // Exercise a malformed custom factory result.
		}, want: "nil migrator"},
		{name: "unsupported", dsn: "dsn", dir: "dir", version: 0, targeted: false, factory: func(testing.TB, string, string, ctxlog.ILogger) (Migrator, error) {
			return upOnlyMigrator{}, nil
		}, want: "RollbackMigrator"},
		{name: "down failure", dsn: "dsn", dir: "dir", version: 0, targeted: false, factory: recorderFactory(operationErr), want: operationErr.Error()},
		{name: "down to failure", dsn: "dsn", dir: "dir", targeted: true, version: 1,
			factory: recorderFactory(operationErr), want: operationErr.Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Arrange a recorder so the expected Fatalf does not fail the parent test.
			tb := &fatalRecorder{TB: t, message: ""}
			done := make(chan struct{})

			// Act in an isolated goroutine to observe Fatalf's termination.
			go func() {
				defer close(done)
				if tc.targeted {
					RollbackMigrationsToVersion(tb, tc.dsn, tc.dir, tc.factory, tc.version)
				} else {
					RollbackMigrations(tb, tc.dsn, tc.dir, tc.factory)
				}
			}()
			<-done

			// Assert that the public helper fails the test with the original cause.
			require.Contains(t, tb.message, tc.want)
		})
	}
}

// recorderFactory supplies a fresh rollback recorder for a public helper test.
func recorderFactory(err error) MigrateFactory {
	return func(testing.TB, string, string, ctxlog.ILogger) (Migrator, error) {
		return &rollbackRecorder{upCalls: 0, downCalls: 0, versions: nil, err: err}, nil
	}
}

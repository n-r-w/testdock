package testdock

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// closeTimeoutCleanupChildEnv selects the subprocess branch for cleanup continuation checks.
	closeTimeoutCleanupChildEnv = "TESTDOCK_CLOSE_TIMEOUT_CLEANUP_CHILD"
	// pgxPoolCloseTimeoutChildEnv selects the subprocess branch for real pgxpool timeout checks.
	pgxPoolCloseTimeoutChildEnv = "TESTDOCK_PGX_POOL_CLOSE_TIMEOUT_CHILD"
)

// TestWithCloseTimeoutValidation verifies the shared close-timeout option contract.
func TestWithCloseTimeoutValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		options       []Option
		wantTimeout   time.Duration
		wantErrSubstr string
	}{
		{
			name:          "default",
			options:       nil,
			wantTimeout:   30 * time.Second,
			wantErrSubstr: "",
		},
		{
			name:          "custom positive timeout",
			options:       []Option{WithCloseTimeout(250 * time.Millisecond)},
			wantTimeout:   250 * time.Millisecond,
			wantErrSubstr: "",
		},
		{
			name:          "zero timeout",
			options:       []Option{WithCloseTimeout(0)},
			wantTimeout:   0,
			wantErrSubstr: "closeTimeout must be greater than 0",
		},
		{
			name:          "negative timeout",
			options:       []Option{WithCloseTimeout(-time.Second)},
			wantTimeout:   0,
			wantErrSubstr: "closeTimeout must be greater than 0",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db := newCloseTimeoutOptionTestDB()
			err := db.prepareOptions("pgx", tt.options)
			if tt.wantErrSubstr != "" {
				require.ErrorContains(t, err, tt.wantErrSubstr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantTimeout, db.closeTimeout)
		})
	}
}

// TestCloseResourceWithTimeoutCompletesBeforeTimeout verifies the success path.
func TestCloseResourceWithTimeoutCompletesBeforeTimeout(t *testing.T) {
	t.Parallel()

	closeCalled := false
	diagnosticsCalled := false
	err := closeResourceWithTimeout(time.Second, func() error {
		closeCalled = true
		return nil
	}, func() string {
		diagnosticsCalled = true
		return "diagnostics must not be collected on success"
	})

	require.NoError(t, err)
	assert.True(t, closeCalled)
	assert.False(t, diagnosticsCalled)
}

// TestCloseResourceWithTimeoutIgnoresCloseErrorPreserves current cleanup behavior for non-timeout errors.
func TestCloseResourceWithTimeoutIgnoresCloseErrorPreserves(t *testing.T) {
	t.Parallel()

	closeCalled := false
	err := closeResourceWithTimeout(time.Second, func() error {
		closeCalled = true
		return errors.New("ordinary close error")
	}, func() string {
		return "diagnostics must not be collected for ordinary close errors"
	})

	require.NoError(t, err)
	assert.True(t, closeCalled)
}

// TestCloseResourceWithTimeoutReturnsTimeoutDiagnostics verifies bounded close failure.
func TestCloseResourceWithTimeoutReturnsTimeoutDiagnostics(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	started := time.Now()
	err := closeResourceWithTimeout(20*time.Millisecond, func() error {
		<-release
		return nil
	}, func() string {
		return "required diagnostics"
	})

	require.Error(t, err)
	assert.Less(t, time.Since(started), time.Second)
	require.ErrorContains(t, err, "close timed out after 20ms")
	require.ErrorContains(t, err, "required diagnostics")
}

// TestCloseTimeoutDiagnosticsIncludesPgxStatsAndGoroutineDump verifies required pgx diagnostics.
func TestCloseTimeoutDiagnosticsIncludesPgxStatsAndGoroutineDump(t *testing.T) {
	t.Parallel()

	diagnostics := formatCloseTimeoutDiagnostics(closeTimeoutDiagnostics{
		TestName:     "TestLeakedConnection",
		Resource:     "pgxpool",
		RedactedDSN:  "postgres://postgres:*****@localhost:5432/t_test?sslmode=disable",
		DatabaseName: "t_test",
		Timeout:      30 * time.Second,
		PgxStats: &pgxPoolCloseStats{
			AcquiredConns:     1,
			IdleConns:         2,
			TotalConns:        3,
			ConstructingConns: 4,
			MaxConns:          5,
		},
		GoroutineDump: "goroutine 1 [running]:\nexample.stack()",
	})

	assert.Contains(t, diagnostics, "test: TestLeakedConnection")
	assert.Contains(t, diagnostics, "resource: pgxpool")
	assert.Contains(t, diagnostics, "dsn: postgres://postgres:*****@localhost:5432/t_test?sslmode=disable")
	assert.Contains(t, diagnostics, "database: t_test")
	assert.Contains(t, diagnostics, "timeout: 30s")
	assert.Contains(t, diagnostics, "acquired_conns: 1")
	assert.Contains(t, diagnostics, "idle_conns: 2")
	assert.Contains(t, diagnostics, "total_conns: 3")
	assert.Contains(t, diagnostics, "constructing_conns: 4")
	assert.Contains(t, diagnostics, "max_conns: 5")
	assert.Contains(t, diagnostics, "goroutine dump:")
	assert.Contains(t, diagnostics, "goroutine 1 [running]")
}

// TestGetPgxPoolCloseTimeoutSubprocess verifies real pgxpool cleanup timeout behavior.
func TestGetPgxPoolCloseTimeoutSubprocess(t *testing.T) {
	if os.Getenv(pgxPoolCloseTimeoutChildEnv) == "1" {
		runGetPgxPoolCloseTimeoutChild(t)
		return
	}

	cmd := exec.CommandContext(t.Context(), os.Args[0],
		"-test.run=^TestGetPgxPoolCloseTimeoutSubprocess$",
		"-test.count=1",
		"-test.timeout=20s",
	)
	cmd.Env = append(os.Environ(), pgxPoolCloseTimeoutChildEnv+"=1")

	output, err := cmd.CombinedOutput()
	require.Error(t, err)

	text := string(output)
	assert.Contains(t, text, "close timed out after 50ms")
	assert.Contains(t, text, "resource: pgxpool")
	assert.Contains(t, text, "acquired_conns: 1")
	assert.Contains(t, text, "goroutine dump:")
	assert.Contains(t, text, "pgx cleanup continuation marker")
}

// TestCloseTimeoutCleanupContinuesAfterError verifies non-fatal cleanup timeout behavior.
func TestCloseTimeoutCleanupContinuesAfterError(t *testing.T) {
	t.Parallel()

	if os.Getenv(closeTimeoutCleanupChildEnv) == "1" {
		runCloseTimeoutCleanupChild(t)
		return
	}

	cmd := exec.CommandContext(t.Context(), os.Args[0],
		"-test.run=^TestCloseTimeoutCleanupContinuesAfterError$",
		"-test.count=1",
	)
	cmd.Env = append(os.Environ(), closeTimeoutCleanupChildEnv+"=1")

	output, err := cmd.CombinedOutput()
	require.Error(t, err)

	text := string(output)
	assert.Contains(t, text, "close timed out after 20ms")
	assert.Contains(t, text, "cleanup continuation marker")
}

// runGetPgxPoolCloseTimeoutChild leaks one acquired pgx connection until after close timeout.
func runGetPgxPoolCloseTimeoutChild(t *testing.T) {
	t.Helper()

	var (
		releaseConn func()
		releaseOnce sync.Once
	)

	t.Cleanup(func() {
		if releaseConn != nil {
			releaseOnce.Do(releaseConn)
		}
		t.Log("pgx cleanup continuation marker")
	})

	pool, _ := GetPgxPool(t,
		DefaultPostgresDSN,
		WithCloseTimeout(50*time.Millisecond),
		WithDockerImage(testPostgresImage),
	)

	conn, err := pool.Acquire(t.Context())
	require.NoError(t, err)
	releaseConn = conn.Release

	go func() {
		time.Sleep(500 * time.Millisecond)
		releaseOnce.Do(conn.Release)
	}()
}

// newCloseTimeoutOptionTestDB creates a database config that runs option validation only.
func newCloseTimeoutOptionTestDB() *testDB {
	return &testDB{
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
}

// runCloseTimeoutCleanupChild creates a failing cleanup and proves later cleanup still runs.
func runCloseTimeoutCleanupChild(t *testing.T) {
	t.Helper()

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	t.Cleanup(func() { t.Log("cleanup continuation marker") })
	t.Cleanup(func() {
		err := closeResourceWithTimeout(20*time.Millisecond, func() error {
			<-release
			return nil
		}, func() string {
			return strings.Join([]string{
				"test: " + t.Name(),
				"resource: test resource",
				fmt.Sprintf("timeout: %s", 20*time.Millisecond),
			}, "\n")
		})
		if err != nil {
			t.Errorf("%v", err)
		}
	})
}

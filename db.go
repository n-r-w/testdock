package testdock

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/n-r-w/ctxlog"
)

// Informer interface for database information.
type Informer interface {
	// DSN returns the real database connection string.
	DSN() string
	// Host returns the host of the database server.
	Host() string
	// Port returns the port of the database server.
	Port() int
	// DatabaseName returns the database name for testing.
	DatabaseName() string
}

const (
	// DefaultRetryTimeout is the default retry timeout.
	DefaultRetryTimeout = time.Second * 3
	// DefaultTotalRetryDuration is the default total retry duration.
	DefaultTotalRetryDuration = time.Second * 30
)

// PrepareCleanUp - function for prepare to delete temporary test database.
// For example, disconnect users.
type PrepareCleanUp func(db *sql.DB, databaseName string) error

// testDB represents a test database.
type testDB struct {
	t testing.TB

	logger ctxlog.ILogger // unified way to logging

	databaseName string // name of the test database
	url          *dbURL // parsed database connection string
	dsnNoPass    string // database connection string without password

	// options
	driver                  string           // database driver (pgx, pq, etc)
	mode                    RunMode          // run mode (docker or external)
	dsn                     string           // database connection string
	retryTimeout            time.Duration    // retry timeout for connecting to the database
	totalRetryDuration      time.Duration    // total retry duration
	migrationsDir           string           // migrations directory
	unsetProxyEnv           bool             // unset HTTP_PROXY, HTTPS_PROXY etc. environment variables
	MigrateFactory          MigrateFactory   // unified way to create a migrations
	prepareCleanUp          []PrepareCleanUp // function for prepare to delete temporary test database.
	connectDatabase         string           // database name for connecting to the database server
	connectDatabaseOverride bool

	dockerPort           int      // docker port
	dockerRepository     string   // docker hub repository
	dockerImage          string   // docker hub image tag
	dockerSocketEndpoint string   // docker socket endpoint for connecting to the docker daemon
	dockerEnv            []string // environment variables for the docker container
}

var (
	globalMu      sync.Mutex
	globalMuByDSN = make(map[string]*sync.Mutex)
)

// newTDB creates a new test database and applies migrations.
func newTDB(ctx context.Context, tb testing.TB, driver, dsn string, opt []Option) *testDB {
	tb.Helper()

	var (
		db = &testDB{
			t:                  tb,
			logger:             ctxlog.Must(ctxlog.WithTesting(tb)),
			driver:             driver,
			dsn:                dsn,
			mode:               RunModeAuto,
			retryTimeout:       DefaultRetryTimeout,
			totalRetryDuration: DefaultTotalRetryDuration,
		}
		errResult error
	)

	defer func() {
		if errResult != nil {
			tb.Fatalf("cannot create test database: %v", errResult)
		}
	}()

	if errResult = db.prepareOptions(driver, opt); errResult != nil {
		return nil
	}

	globalMu.Lock()
	mu, ok := globalMuByDSN[db.dsn]
	if !ok {
		mu = &sync.Mutex{}
		globalMuByDSN[db.dsn] = mu
	}
	globalMu.Unlock()

	mu.Lock()
	defer mu.Unlock()

	if db.mode == RunModeDocker {
		db.logger.Info(ctx, "using docker test database", "dsn", db.dsnNoPass)
		if errResult = db.createDockerResources(ctx); errResult != nil {
			return nil
		}
	} else {
		db.logger.Info(ctx, "using real test database", "dsn", db.dsnNoPass)
	}

	if errResult = db.createTestDatabase(ctx); errResult != nil {
		if err := db.close(ctx); err != nil {
			db.logger.Info(ctx, "failed to close test database", "dsn", db.dsnNoPass, "error", err)
		}
		return nil
	}

	if db.migrationsDir != "" {
		if errResult = db.migrationsUp(ctx); errResult != nil {
			return nil
		}
	}

	tb.Cleanup(func() {
		ctx := context.Background()
		if err := db.close(ctx); err != nil {
			db.logger.Info(ctx, "failed to close test database", "dsn", db.dsnNoPass, "error", err)
		} else {
			db.logger.Info(ctx, "test database closed", "dsn", db.dsnNoPass)
		}
	})

	return db
}

// migrationsUp applies migrations to the database.
func (d *testDB) migrationsUp(ctx context.Context) error {
	d.logger.Info(ctx, "migrations up start", "dsn", d.dsnNoPass)
	defer d.logger.Info(ctx, "migrations up end", "dsn", d.dsnNoPass)

	dsn := d.url.replaceDatabase(d.databaseName).string(false)

	migrator, err := d.MigrateFactory(d.t, dsn, d.migrationsDir, d.logger)
	if err != nil {
		return fmt.Errorf("new migrator: %w", err)
	}

	if err = migrator.Up(context.Background()); err != nil {
		return fmt.Errorf("up migrations: %w", err)
	}

	return nil
}

// close closes the test database.
func (d *testDB) close(ctx context.Context) error {
	if d.mode != RunModeDocker {
		if d.driver == mongoDriverName {
			return nil
		}

		// remove the database created before applying the migrations
		d.logger.Info(ctx, "deleting test database", "dsn", d.dsnNoPass, "database", d.databaseName)

		dsn := d.url.string(false)
		db, err := sql.Open(d.driver, dsn)
		if err != nil {
			return fmt.Errorf("sql open url (%s): %w", dsn, err)
		}
		defer func() {
			_ = db.Close()
		}()

		for _, prepareCleanUp := range d.prepareCleanUp {
			if err := prepareCleanUp(db, d.databaseName); err != nil {
				d.logger.Info(ctx, "failed to prepare clean up", "dsn", d.dsnNoPass, "error", err)
			}
		}

		if _, err = db.Exec(fmt.Sprintf("DROP DATABASE %s", d.databaseName)); err != nil {
			return fmt.Errorf("drop db: %w", err)
		}

		d.logger.Info(ctx, "test database deleted", "dsn", d.dsnNoPass, "database", d.databaseName)
	}

	return nil
}

// initDatabase creates a test database or connects to an existing one.
func (d *testDB) createTestDatabase(ctx context.Context) error {
	if d.driver == mongoDriverName {
		return nil
	}

	return d.createSQLDatabase(ctx)
}

// retryConnect connects to the database with retries.
func (d *testDB) retryConnect(ctx context.Context, info string, op func() error) error {
	var attempt int
	operation := func() (struct{}, error) {
		if err := op(); err != nil {
			d.logger.Info(ctx, "retrying operation", "info", info, "attempt", attempt, "error", err)
			attempt++
			return struct{}{}, err
		}
		return struct{}{}, nil
	}

	_, err := backoff.Retry(
		context.Background(), operation,
		backoff.WithBackOff(backoff.NewConstantBackOff(d.retryTimeout)),
		backoff.WithMaxElapsedTime(d.totalRetryDuration),
	)
	if err != nil {
		return fmt.Errorf("retry failed after %d attempts: %w", attempt, err)
	}

	return nil
}

// DSN returns the real database connection string.
func (d *testDB) DSN() string {
	return d.url.replaceDatabase(d.databaseName).string(false)
}

// Host returns the database host.
func (d *testDB) Host() string {
	return d.url.Host
}

// Port returns the database port.
func (d *testDB) Port() int {
	return d.url.Port
}

// DatabaseName returns the database name for testing.
func (d *testDB) DatabaseName() string {
	return d.databaseName
}

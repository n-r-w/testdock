package testdock

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v5"
)

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

	logger Logger // unified way to logging

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
func newTDB(tb testing.TB, driver, dsn string, opt []Option) *testDB {
	tb.Helper()

	var (
		db = &testDB{
			t:                  tb,
			logger:             NewDefaultLogger(tb),
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
			db.logger.Fatalf("%v", errResult)
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
		db.logger.Logf("[%s] using docker test database", db.dsnNoPass)
		if errResult = db.createDockerResources(); errResult != nil {
			return nil
		}
	} else {
		db.logger.Logf("[%s] using real test database", db.dsnNoPass)
	}

	if errResult = db.createTestDatabase(); errResult != nil {
		if err := db.close(); err != nil {
			db.logger.Logf("[%s] failed to close test database: %v", db.dsnNoPass, err)
		}
		return nil
	}

	if db.migrationsDir != "" {
		if errResult = db.migrationsUp(); errResult != nil {
			return nil
		}
	}

	tb.Cleanup(func() {
		if err := db.close(); err != nil {
			db.logger.Logf("[%s] failed to close test database: %v", db.dsnNoPass, err)
		} else {
			db.logger.Logf("[%s] test database closed", db.dsnNoPass)
		}
	})

	return db
}

// migrationsUp applies migrations to the database.
func (d *testDB) migrationsUp() error {
	d.logger.Logf("[%s] migrations up start", d.dsnNoPass)
	defer d.logger.Logf("[%s] migrations up end", d.dsnNoPass)

	dsn := d.url.replaceDatabase(d.databaseName).string(false)

	migrator, err := d.MigrateFactory(dsn, d.migrationsDir, d.logger)
	if err != nil {
		return fmt.Errorf("new migrator: %w", err)
	}

	if err = migrator.Up(context.Background()); err != nil {
		return fmt.Errorf("up migrations: %w", err)
	}

	return nil
}

// close closes the test database.
func (d *testDB) close() error {
	if d.mode != RunModeDocker {
		// remove the database created before applying the migrations
		d.logger.Logf("[%s] deleting test database %s", d.dsnNoPass, d.databaseName)

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
				d.logger.Logf("[%s] failed to prepare clean up: %v", d.dsnNoPass, err)
			}
		}

		if _, err = db.Exec(fmt.Sprintf("DROP DATABASE %s", d.databaseName)); err != nil {
			return fmt.Errorf("drop db: %w", err)
		}

		d.logger.Logf("[%s] test db %s deleted", d.dsnNoPass, d.databaseName)
	}

	return nil
}

// initDatabase creates a test database or connects to an existing one.
func (d *testDB) createTestDatabase() error {
	if d.driver == mongoDriverName {
		return nil
	}

	return d.createSQLDatabase()
}

// retryConnect connects to the database with retries.
func (d *testDB) retryConnect(info string, op func() error) error {
	var attempt int
	operation := func() (struct{}, error) {
		if err := op(); err != nil {
			d.logger.Logf("[%s] retrying attempt %d: %v", info, attempt, err)
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

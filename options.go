package testdock

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	// DefaultMongoDSN - default mongodb connection string.
	DefaultMongoDSN = "mongodb://testuser:secret@127.0.0.1:27017/testdb?authSource=admin"
	// DefaultMysqlDSN - default mysql connection string.
	DefaultMysqlDSN = "root:secret@tcp(127.0.0.1:3306)/test_db"
	// DefaultPostgresDSN - default postgres connection string.
	DefaultPostgresDSN = "postgres://postgres:secret@127.0.0.1:5432/postgres?sslmode=disable"
)

// RunMode defines the run mode of the test database.
type RunMode int

const (
	// RunModeUnknown - unknown run mode
	RunModeUnknown RunMode = 0
	// RunModeDocker - run the tests in docker
	RunModeDocker RunMode = 1
	// RunModeExternal - run the tests in external database
	RunModeExternal RunMode = 2
	// RunModeAuto - checks the environment variable TESTDOCK_DSN_[DRIVER]. If it is set,
	// then RunModeExternal, otherwise RunModeDocker.
	// If TESTDOCK_DSN_[DRIVER] is set and RunModeAuto, WithDSN option is ignored.
	// For example, for postgres pgx driver:
	//   TESTDOCK_DSN_PGX=postgres://postgres:secret@localhost:5432/postgres&sslmode=disable
	RunModeAuto RunMode = 3
)

// Option option for creating a test database.
type Option func(*testDB)

// WithMode sets the mode for the test database.
// The default is RunModeAuto.
func WithMode(mode RunMode) Option {
	return func(o *testDB) {
		o.mode = mode
	}
}

// WithDockerRepository sets the name of docker hub repository.
// Required for RunModeDocker or RunModeAuto with empty environment variable TESTDOCK_DSN_[DRIVER].
func WithDockerRepository(dockerRepository string) Option {
	return func(o *testDB) {
		o.dockerRepository = dockerRepository
	}
}

// WithDockerImage sets the name of the docker image.
// The default is `latest`.
func WithDockerImage(dockerImage string) Option {
	return func(o *testDB) {
		o.dockerImage = dockerImage
	}
}

// WithDockerSocketEndpoint sets the docker socket endpoint for connecting to the docker daemon.
// The default is autodetect.
func WithDockerSocketEndpoint(dockerSocketEndpoint string) Option {
	return func(o *testDB) {
		o.dockerSocketEndpoint = dockerSocketEndpoint
	}
}

// WithDockerPort sets the port for connecting to database in docker.
// The default is the port from the DSN.
func WithDockerPort(dockerPort int) Option {
	return func(o *testDB) {
		o.dockerPort = dockerPort
	}
}

// WithRetryTimeout sets the timeout for connecting to the database.
// The default is 30 seconds.
func WithRetryTimeout(retryTimeout time.Duration) Option {
	return func(o *testDB) {
		o.retryTimeout = retryTimeout
	}
}

// WithLogger sets the logger for the test database.
// The default is logger from testing.TB.
func WithLogger(logger Logger) Option {
	return func(o *testDB) {
		o.logger = logger
	}
}

// WithMigrations sets the directory and factory for the migrations.
func WithMigrations(migrationsDir string, migrateFactory MigrateFactory) Option {
	return func(o *testDB) {
		o.migrationsDir = migrationsDir
		o.MigrateFactory = migrateFactory
	}
}

// WithDockerEnv sets the environment variables for the docker container.
// The default is empty.
func WithDockerEnv(dockerEnv []string) Option {
	return func(o *testDB) {
		o.dockerEnv = dockerEnv
	}
}

// WithUnsetProxyEnv unsets the proxy environment variables.
// The default is false.
func WithUnsetProxyEnv(unsetProxyEnv bool) Option {
	return func(o *testDB) {
		o.unsetProxyEnv = unsetProxyEnv
	}
}

// WithPrepareCleanUp sets the function for prepare to delete temporary test database.
// The default is empty, but `GetPgxPool` and `GetPqConn` use it
// to automatically apply cleanup handlers to disconnect all users from the database
// before cleaning up.
func WithPrepareCleanUp(prepareCleanUp PrepareCleanUp) Option {
	return func(o *testDB) {
		o.prepareCleanUp = append(o.prepareCleanUp, prepareCleanUp)
	}
}

// WithConnectDatabase sets the name of the database to connect to.
// The default will be take from the DSN.
func WithConnectDatabase(connectDatabase string) Option {
	return func(o *testDB) {
		o.connectDatabase = connectDatabase
		o.connectDatabaseOverride = true
	}
}

func (d *testDB) prepareOptions(driver string, options []Option) error {
	for _, o := range options {
		o(d)
	}

	if d.driver == "" {
		return errors.New("driver is empty")
	}

	if d.mode == RunModeAuto {
		dsnEnv := os.Getenv(fmt.Sprintf("TESTDOCK_DSN_%s", strings.ToUpper(driver)))
		if dsnEnv != "" {
			d.dsn = dsnEnv
			d.mode = RunModeExternal
		} else {
			d.mode = RunModeDocker
		}
	}

	if d.dsn == "" {
		return errors.New("dsn is empty")
	}

	p, err := parseURL(d.dsn)
	if err != nil {
		return fmt.Errorf("parse dsn: %w", err)
	}
	d.url = p
	d.dsnNoPass = p.string(true)

	if !d.connectDatabaseOverride && d.connectDatabase == "" {
		d.connectDatabase = p.Database
	}

	if d.mode == RunModeDocker {
		if d.dockerRepository == "" {
			return errors.New("dockerRepository is empty")
		}
		if d.dockerImage == "" {
			d.dockerImage = "latest"
		}
		if d.dockerPort <= 0 {
			d.dockerPort = p.Port
			if d.dockerPort <= 0 {
				return errors.New("dockerPort must be greater than 0")
			}
		}
	}

	dbName := fmt.Sprintf("t_%s_%s", time.Now().Format("2006_0102_1504_05"), uuid.New().String())
	d.databaseName = strings.ReplaceAll(dbName, "-", "")

	if (d.MigrateFactory == nil) != (d.migrationsDir == "") {
		return errors.New("MigrateFactory and migrationsDir must be set together")
	}

	return nil
}

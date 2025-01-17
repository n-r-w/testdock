package pglive

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/pressly/goose/v3"
	"go.uber.org/multierr"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx postgres driver
)

const (
	defaultDockerImage       = "latest"
	defaultDockerPortMapping = "5433"
	defaultRetryTimeout      = time.Second * 30
	defaultHost              = "127.0.0.1"
	defaultPort              = "5432"
	defaultUser              = "postgres"
	defaultPassword          = "secret"
)

// TestDB creates a connection to a temporary test cluster databasepg, allows you to deploy migrations on it and run tests.
type TestDB struct {
	t              testing.TB
	options        testDBOptions
	dockerResource *dockertest.Resource
	dockerPool     *dockertest.Pool
	url            string
	deleteDBname   string
}

// Option option for creating a test database.
type Option func(*testDBOptions)

// WithHost sets the host for connecting to the database.
// The default is 127.0.0.1
func WithHost(host string) Option {
	return func(o *testDBOptions) {
		o.host = host
	}
}

// WithPort sets the port for connecting to the database.
// The default is 5432.
func WithPort(port string) Option {
	return func(o *testDBOptions) {
		o.port = port
	}
}

// WithDatabase sets the database name.
// The default is a randomly generated name.
func WithDatabase(database string) Option {
	return func(o *testDBOptions) {
		o.database = database
	}
}

// WithUser sets the user name.
// The default is "postgres".
func WithUser(user string) Option {
	return func(o *testDBOptions) {
		o.user = user
	}
}

// WithPassword sets the password.
// The default is "secret".
func WithPassword(password string) Option {
	return func(o *testDBOptions) {
		o.password = password
	}
}

// WithDockerImage sets the name of the postgres image from https://hub.docker.com/_/postgres when running locally in docker.
// The default is latest.
func WithDockerImage(dockerImage string) Option {
	return func(o *testDBOptions) {
		o.dockerImage = dockerImage
	}
}

// WithDockerPortMapping sets the port for mapping postgres to the host when running locally in docker.
// The default is 5433.
func WithDockerPortMapping(dockerPortMapping string) Option {
	return func(o *testDBOptions) {
		o.dockerPortMapping = dockerPortMapping
	}
}

// WithDockerSocketEndpoint sets the docker socket endpoint for connecting to the docker daemon.
func WithDockerSocketEndpoint(dockerSocketEndpoint string) Option {
	return func(o *testDBOptions) {
		o.dockerSocketEndpoint = dockerSocketEndpoint
	}
}

// WithForceDockerMode forces the tests to run in docker.
func WithForceDockerMode() Option {
	return func(o *testDBOptions) {
		o.forceDockerMode = true
	}
}

// WithRetryTimeout sets the timeout for connecting to the database.
// The default is 30 seconds.
func WithRetryTimeout(retryTimeout time.Duration) Option {
	return func(o *testDBOptions) {
		o.retryTimeout = retryTimeout
	}
}

// WithLogger sets the logger for the test database.
func WithLogger(logger Logger) Option {
	return func(o *testDBOptions) {
		o.logger = logger
	}
}

type testDBOptions struct {
	logger               Logger
	retryTimeout         time.Duration
	dockerImage          string
	dockerPortMapping    string
	dockerSocketEndpoint string
	forceDockerMode      bool

	host     string
	port     string
	database string
	user     string
	password string
}

var (
	muDB    sync.Mutex
	muGoose sync.Mutex
)

// GetPool inits a test database, applies migrations, and returns a connection pool to the database.
func GetPool(tb testing.TB, migrationsDir string, opt ...Option) *pgxpool.Pool {
	tb.Helper()

	options := testDBOptions{}
	for _, o := range opt {
		o(&options)
	}

	if options.logger == nil {
		options.logger = defaultLogger{tb}
	}

	tDB, err := NewTDB(tb, opt...)
	if err != nil {
		options.logger.Fatal(err)
	}

	if err = tDB.MigrationsUp(migrationsDir, ""); err != nil {
		options.logger.Fatal(err)
	}

	db, err := tDB.connectDB(tDB.DSN(), options.retryTimeout)
	if err != nil {
		options.logger.Fatal(err)
	}

	tb.Cleanup(func() { db.Close() })

	// we do not roll back migrations, because the test database is deleted after the test
	return db
}

// NewTDB creates a new test database.
func NewTDB(tb testing.TB, opt ...Option) (*TestDB, error) { //nolint:gocognit
	tb.Helper()

	options := testDBOptions{}
	for _, o := range opt {
		o(&options)
	}

	muDB.Lock()
	defer muDB.Unlock()

	var (
		db  *TestDB
		err error

		dockerMode = true
		createDB   = false
	)

	if options.dockerImage == "" {
		options.dockerImage = defaultDockerImage
	}
	if options.retryTimeout <= 0 {
		options.retryTimeout = defaultRetryTimeout
	}
	if options.dockerPortMapping == "" {
		options.dockerPortMapping = defaultDockerPortMapping
	}

	// if at least one environment variable is not empty or the value of the parameter is explicitly specified, then we run the tests in "not in docker" mode
	if options.host == "" {
		if options.host = os.Getenv("POSTGRES_HOST"); options.host == "" {
			options.host = defaultHost
		} else {
			dockerMode = false
		}
	} else {
		dockerMode = false
	}

	if options.port == "" {
		if options.port = os.Getenv("POSTGRES_PORT"); options.port == "" {
			options.port = defaultPort
		} else {
			dockerMode = false
		}
	} else {
		dockerMode = false
	}

	if options.database == "" {
		if options.database = os.Getenv("POSTGRES_DB"); options.database == "" {
			// generate a random database name
			// At the beginning, we add the current time to make it easy to find a fresh database
			// if the old ones were not deleted correctly when stopping the tests under the debugger
			dbName := fmt.Sprintf("t_%s_%s", time.Now().Format("2006_0102_1504_05"), uuid.New().String())
			options.database = strings.ReplaceAll(dbName, "-", "")
			createDB = true
		} else {
			dockerMode = false
		}
	} else {
		dockerMode = false
	}

	if options.user == "" {
		if options.user = os.Getenv("POSTGRES_USER"); options.user == "" {
			options.user = defaultUser
		} else {
			dockerMode = false
		}
	} else {
		dockerMode = false
	}

	if options.password == "" {
		if options.password = os.Getenv("POSTGRES_PASSWORD"); options.password == "" {
			options.password = defaultPassword
		} else {
			dockerMode = false
		}
	} else {
		dockerMode = false
	}

	if options.forceDockerMode {
		dockerMode = true
	}

	if dockerMode {
		if options.dockerSocketEndpoint == "" {
			options.dockerSocketEndpoint = os.Getenv("DOCKER_SOCKET_ENDPOINT")
		}

		options.logger.Log("using docker test db")
		db, err = newDockerTestDB(tb, options.logger, options.dockerImage, options.dockerPortMapping, options.database, options.dockerSocketEndpoint, options.retryTimeout)
	} else {
		options.logger.Log("using real test db")
		db, err = newRealTestDB(tb, options.user, options.password, options.host, options.port, options.database, options.retryTimeout, createDB)
	}

	if db != nil {
		db.options = options
		if createDB && !dockerMode {
			db.deleteDBname = options.database
		}

		tb.Cleanup(func() { db.logClose() })
	}

	return db, err
}

// DSN returns the data source name for connecting to the database.
func (d *TestDB) DSN() string {
	return d.url
}

// MigrationsUp applies migrations to the database.
func (d *TestDB) MigrationsUp(migrationsDir, schema string) error {
	d.options.logger.Log("migrations up start")
	defer d.options.logger.Log("migrations up end")

	muGoose.Lock()
	defer muGoose.Unlock()

	dsn := d.url
	if schema != "" {
		dsn += "&options=" + url.QueryEscape("-c search_path="+schema)
	}

	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("sql open postgres url (%s): %w", dsn, err)
	}
	defer conn.Close()

	if schema != "" {
		if _, err := conn.Exec(fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %q`, schema)); err != nil {
			return fmt.Errorf("create schema: %w", err)
		}
	}

	return goose.Up(conn, migrationsDir)
}

// Close closes the test database.
func (d *TestDB) Close() (err error) {
	if d.dockerPool != nil {
		d.dockerPool = nil
	}

	if d.deleteDBname != "" {
		// remove the database created before applying the migrations
		d.options.logger.Logf("deleting test db %s", d.deleteDBname)

		db, err1 := d.connectPostgresDB(d.options.user, d.options.password, d.options.host, d.options.port, d.options.retryTimeout)
		defer func() {
			db.Close()
		}()
		if err1 != nil {
			err = multierr.Append(err, err1)
		} else {
			// disconnect users (for example, if the developer got into the database through psql and does not allow it to be deleted)
			ctx := context.Background()

			_, err1 = db.Exec(ctx,
				fmt.Sprintf(`SELECT pg_terminate_backend(pg_stat_activity.pid) 
				FROM pg_stat_activity 
				WHERE pg_stat_activity.datname = '%s' AND pid <> pg_backend_pid()`, d.deleteDBname))
			if err1 != nil {
				err = multierr.Append(err, err1)
			}

			_, err1 = db.Exec(ctx, fmt.Sprintf("DROP DATABASE %s", d.deleteDBname))
			if err1 != nil {
				err = multierr.Append(err, err1)
			}
		}

		d.deleteDBname = ""
		if err1 == nil {
			d.options.logger.Logf("test db %s deleted", d.deleteDBname)
		}
	}

	if err != nil {
		return fmt.Errorf("close: %w", err)
	}

	return nil
}

func (d *TestDB) logClose() {
	if err := d.Close(); err != nil {
		d.options.logger.Logf("failed to close test db: %v", err)
	} else {
		d.options.logger.Log("test db closed")
	}
}

// we ensure the creation of docker resources only once for all tests
var (
	globalDockerMu       sync.Mutex
	globalDockerCount    int
	globalDockerResource *dockertest.Resource
	globalDockerPool     *dockertest.Pool
)

const (
	dockerPgPort   = "5432/tcp"
	dockerUserName = "postgres"
	dockerPassword = "secret"
)

// getDockerResources returns a pool and a resource for creating a test database in docker.
func getDockerResources(tb testing.TB, logger Logger, postgresImage, hostPort, databaseName, dockerSocketEndpoint string) (*dockertest.Pool, *dockertest.Resource, error) { //nolint:gocognit
	globalDockerMu.Lock()
	defer globalDockerMu.Unlock()

	tb.Cleanup(func() {
		globalDockerMu.Lock()
		defer globalDockerMu.Unlock()
		globalDockerCount--

		if globalDockerCount == 0 {
			_ = globalDockerPool.Purge(globalDockerResource) // error is not important
			logger.Log("dockertest resources purged")
		}
	})

	if globalDockerCount == 0 {
		var err error
		globalDockerPool, err = dockertest.NewPool(dockerSocketEndpoint)
		if err != nil {
			return nil, nil, fmt.Errorf("dockertest NewPool: %w", err)
		}

		// we clear the proxy environment variables, because they can interfere with the work of docker
		proxyEnv := []string{
			"HTTP_PROXY",
			"HTTPS_PROXY",
			"ALL_PROXY",
			"http_proxy",
			"https_proxy",
			"all_proxy",
		}
		for _, env := range proxyEnv {
			if os.Getenv(env) != "" {
				logger.Logf("unset proxy env %s", env)
				_ = os.Unsetenv(env)
			}
		}

		err = globalDockerPool.Client.Ping()
		if err != nil {
			globalDockerPool = nil
			return nil, nil, fmt.Errorf("dockertest ping: %w", err)
		}

		// docker releases the port after calling globalDockerPool.Purge(globalDockerResource) not instantly, so we try several times
		const (
			maxAttempts = 10
			sleepTime   = 5 * time.Second
		)

		var attempt int
		for {
			globalDockerResource, err = globalDockerPool.RunWithOptions(&dockertest.RunOptions{
				Repository: "postgres",
				Tag:        postgresImage,
				Env: []string{
					fmt.Sprintf("POSTGRES_USER=%s", dockerUserName),
					fmt.Sprintf("POSTGRES_PASSWORD=%s", dockerPassword),
					fmt.Sprintf("POSTGRES_DB=%s", "fake-test-db-not-use"),
					"listen_addresses = '*'",
					"max_connections = 1000",
				},
				PortBindings: map[docker.Port][]docker.PortBinding{
					dockerPgPort: {{
						HostIP:   "127.0.0.1",
						HostPort: hostPort,
					}},
				},
			}, func(config *docker.HostConfig) {
				config.AutoRemove = true
				config.RestartPolicy = docker.RestartPolicy{Name: "no"}
			})

			if err == nil {
				break
			}

			bindErrors := []string{
				"bind: address already in use",
				"port is already allocated",
			}
			needNextPort := false
			for _, bindError := range bindErrors {
				if strings.Contains(err.Error(), bindError) {
					needNextPort = true
					break
				}
			}
			if needNextPort {
				// increase hostPort by 1
				var port int
				port, err = strconv.Atoi(hostPort)
				if err != nil {
					globalDockerPool = nil
					return nil, nil, fmt.Errorf("dockertest RunWithOptions: %w", err)
				}
				logger.Logf("port is already allocated, try next port %d, database %s", port, databaseName)
				hostPort = strconv.Itoa(port + 1)
				continue
			}

			attempt++
			if attempt >= maxAttempts {
				break
			}

			logger.Logf("dockertest RunWithOptions failed, database %s, attempt %d, error %v", databaseName, attempt, err)
			time.Sleep(sleepTime)
		}

		if err != nil {
			globalDockerPool = nil
			return nil, nil, fmt.Errorf("dockertest RunWithOptions: %w", err)
		}

		logger.Logf("dockertest resources created, database %s", databaseName)
	} else {
		logger.Logf("dockertest using existing resources, database %s", databaseName)
	}

	globalDockerCount++
	return globalDockerPool, globalDockerResource, nil
}

// newDockerTestDB creates a test database in docker.
func newDockerTestDB(tb testing.TB, logger Logger, postgresImage, hostPort, databaseName, dockerSocketEndpoint string, retryTimeout time.Duration) (*TestDB, error) {
	pool, resource, err := getDockerResources(tb, logger, postgresImage, hostPort, databaseName, dockerSocketEndpoint)
	if err != nil {
		return nil, err
	}

	d := &TestDB{
		dockerPool:     pool,
		dockerResource: resource,
		t:              tb,
	}

	err = d.initDatabase(dockerUserName, dockerPassword, d.dockerResource.GetBoundIP(dockerPgPort), d.dockerResource.GetPort(dockerPgPort), databaseName,
		retryTimeout, true)
	if err != nil {
		_ = d.Close() // we need to clean up docker resources
		return nil, err
	}
	return d, nil
}

// newRealTestDB creates a test database on the host machine.
func newRealTestDB(tb testing.TB, userName, password, host, port, databaseName string, retryTimeout time.Duration, createDB bool) (*TestDB, error) {
	var (
		err error
		d   = &TestDB{
			t: tb,
		}
	)

	err = d.initDatabase(userName, password, host, port, databaseName, retryTimeout, createDB)
	if err != nil {
		return nil, err
	}
	return d, nil
}

// connectDB connects to the database with retries.
func (d *TestDB) connectDB(dbURL string, retryTimeout time.Duration) (*pgxpool.Pool, error) {
	d.options.logger.Logf("connecting to test db %s", dbURL)

	var (
		db  *pgxpool.Pool
		ctx = context.Background()
	)
	err := retryConnect(retryTimeout, func() (err error) {
		db, err = pgxpool.New(ctx, dbURL)
		if err != nil {
			return err
		}
		if err = db.Ping(ctx); err != nil {
			db.Close()
			return err
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("connect postgres url (%s): %w", dbURL, err)
	}

	return db, nil
}

// connectPostgresDB connects to the postgres database.
func (d *TestDB) connectPostgresDB(userName, password, host, port string, retryTimeout time.Duration) (*pgxpool.Pool, error) {
	dbURL := postgresURL(userName, password, host, port, "postgres")
	return d.connectDB(dbURL, retryTimeout)
}

// initDatabase creates a test database or connects to an existing one.
func (d *TestDB) initDatabase(userName, password, host, port, databaseName string,
	retryTimeout time.Duration, createDB bool,
) (err error) {
	if createDB {
		d.options.logger.Logf("creating new test db %s", databaseName)

		postgresDB, err := d.connectPostgresDB(userName, password, host, port, retryTimeout)
		if err != nil {
			return err
		}
		defer postgresDB.Close()

		_, err = postgresDB.Exec(context.Background(), fmt.Sprintf("CREATE DATABASE %s", databaseName))
		if err != nil {
			return fmt.Errorf("create db: %w", err)
		}

		d.options.logger.Logf("new test db %s created", databaseName)
	}

	d.url = postgresURL(userName, password, host, port, databaseName)

	return nil
}

// retryConnect connects to the database with retries.
func retryConnect(maxTime time.Duration, op func() error) error {
	const retryTimeout = time.Second * 5

	if maxTime == 0 {
		maxTime = time.Minute
	}
	bo := backoff.NewExponentialBackOff()
	bo.MaxInterval = retryTimeout
	bo.MaxElapsedTime = maxTime
	if err := backoff.Retry(op, bo); err != nil {
		if bo.NextBackOff() == backoff.Stop {
			return fmt.Errorf("reached retry deadline %w", err)
		}
		return err
	}

	return nil
}

func postgresURL(userName, password, host, port, databaseName string) string {
	//nolint:nosprintfhostport
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", userName, password, host, port, databaseName)
}

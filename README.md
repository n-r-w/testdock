[![Go Reference](https://pkg.go.dev/badge/github.com/n-r-w/testdock.svg)](https://pkg.go.dev/github.com/n-r-w/testdock/v2)
[![Go Coverage](https://github.com/n-r-w/testdock/wiki/coverage.svg)](https://raw.githack.com/wiki/n-r-w/testdock/coverage.html)
![CI Status](https://github.com/n-r-w/testdock/actions/workflows/go.yml/badge.svg)

# TestDock

TestDock is a Go library that simplifies database testing by providing an easy way to create and manage test databases in realistic scenarios, instead of using mocks. It supports running tests against both Docker containers and external databases, with built-in support for MongoDB and various SQL databases.

## Features

- **Multiple Database Support**
  - MongoDB: `GetMongoDatabase` function
  - PostgreSQL: `GetPgxPool`, `GetPqConn`, and reusable migrated templates for `pgx` tests
  - MySQL: `GetMySQLConn` function
  - Any other SQL database supported by `database/sql` <https://go.dev/wiki/SQLDrivers>: `GetSQLConn` function

- **Flexible Test Environment**
  - Docker container support for isolated testing
  - External database support for CI/CD environments
  - Auto-mode that switches based on environment variables

- **Database Migration Support**
  - Integration with [goose](https://github.com/pressly/goose)
  - Integration with [golang-migrate](https://github.com/golang-migrate/migrate)
  - User provided migration tool
  - Automatic migration application during test setup

- **Robust Connection Handling**
  - Automatic retry mechanisms
  - Automatic selection of a free host port when deploying containers
  - Graceful cleanup after tests

## Installation

```bash
go get github.com/n-r-w/testdock/v2@latest
```

## Core Functions

- `GetPgxPool`: PostgreSQL connection pool (pgx driver)
- `NewPostgresTemplate`: Parent-owned migrated PostgreSQL source for fast physical clones
- `GetPqConn`: PostgreSQL connection (libpq driver)
- `GetMySQLConn`: MySQL connection
- `GetSQLConn`: Generic SQL database connection
- `GetMongoDatabase`: MongoDB database

## Usage

### Connection string format

The connection string format is driver-specific. For example:

- For PostgreSQL: `postgres://user:password@localhost:5432/database?sslmode=disable`
- For MySQL: `root:password@tcp(localhost:3306)/database?parseTime=true`
- For MongoDB: `mongodb://user:password@localhost:27017/database`

### Connection string purpose

Depending on the chosen mode (`WithMode`), the connection string is used differently:

#### `RunModeExternal`

- The connection string is used directly to connect to the database

#### `RunModeDocker`

- The connection string is used to generate the Docker container configuration
- The port value is used 1) as the port inside the container, 2) as the external access port to the database
- If this port is already taken on the host, then TestDock tries to find a free port by incrementing its value by 1 until a free port is found

#### `RunModeAuto` (used by default)

- If the environment variable `TESTDOCK_DSN_<DRIVER_NAME>` is not set, then TestDock chooses
the `RunModeDocker` mode and uses the input string as the container configuration
- If the environment variable `TESTDOCK_DSN_<DRIVER_NAME>` is set, then TestDock chooses
the `RunModeExternal` mode and uses the string from the environment variable to connect to the external database. In this case, the `dsn` parameter of the constructor function is ignored.
Thus, in this mode, the `dsn` parameter is used as a fallback if the environment variable is not set.

### Parallel tests

Each `Get...` call creates an independent temporary database, so the call can be made inside a test that uses `t.Parallel()`. Keep the returned resource in that test; do not share it with other tests. TestDock registers cleanup through `testing.TB.Cleanup`.

In Docker mode, calls with the same resolved DSN reuse one container. TestDock runs at most four database lifecycle operations concurrently per test process and DSN. Creation, automatic migrations, and cleanup share this limit, so waiting operations do not consume database connections. Separate `go test` package processes do not share the limiter. PostgreSQL cleanup retries `DROP DATABASE` after SQLSTATE `53300` using `WithRetryTimeout` and `WithTotalRetryDuration`; an exhausted cleanup error is logged without changing the test result.

When a parent test has many PostgreSQL children with identical migrations and initial data, use `NewPostgresTemplate`. The parent prepares one source database, and each child receives an isolated physical clone through `PostgresTemplate.GetPgxPool`. The source remains alive until the parent and all its subtests complete.

### PostgreSQL Example (using pgx)

```go
import (
    "testing"

    "github.com/n-r-w/testdock/v2"
)

func TestDatabase(t *testing.T) {
    t.Parallel()

    pool, _ := testdock.GetPgxPool(t,
        testdock.DefaultPostgresDSN,
        testdock.WithMigrations("migrations", testdock.GooseMigrateFactoryPGX),
    )

    // Prepare isolated data, run the code under test, and assert through pool.
    // The pool and temporary database are cleaned up automatically.
}
```

### Reusing PostgreSQL migrations across parallel tests

```go
import (
    "testing"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/n-r-w/testdock/v2"
)

func TestDatabaseGroup(t *testing.T) {
    template := testdock.NewPostgresTemplate(
        t,
        testdock.DefaultPostgresDSN,
        testdock.WithPostgresTemplateOptions(
            testdock.WithMigrations("migrations", testdock.GooseMigrateFactoryPGX),
        ),
        testdock.WithPostgresTemplateSetup(func(
            tb testing.TB,
            pool *pgxpool.Pool,
            _ testdock.Informer,
        ) {
            // Add shared seed data once. The setup pool is closed before cloning starts.
        }),
    )

    for _, name := range []string{"first", "second"} {
        t.Run(name, func(t *testing.T) {
            t.Parallel()

            pool, _ := template.GetPgxPool(t)
            // Mutations are isolated from every other clone.
        })
    }
}
```

`WithPostgresTemplateOptions` applies existing database options only to the source. Automatic migrations and `WithPostgresTemplateSetup` run once. PostgreSQL requires the source database to have no open connections while it is copied, so TestDock closes the setup pool before returning the template.

`CREATE DATABASE ... TEMPLATE` copies database objects and data, but PostgreSQL does not copy database-level `GRANT` permissions or settings created through `ALTER DATABASE`.

### MongoDB Example

```go
import (
    "testing"
    "github.com/n-r-w/testdock/v2"
)

func TestMongoDB(t *testing.T) {
    // Get a connection to a test database
    db, _ := testdock.GetMongoDatabase(t, testdock.DefaultMongoDSN,
        testdock.WithMode(testdock.RunModeDocker),
        testdock.WithMigrations("migrations", testdock.GolangMigrateFactory),
    )

    // Use the database for your tests
    // The database will be automatically cleaned up after the test
}
```

## Configuration

### Environment Variables, used by `RunModeAuto`

- `TESTDOCK_DSN_PGX`, `TESTDOCK_DSN_POSTGRES` - PostgreSQL-specific connection strings
- `TESTDOCK_DSN_MYSQL` - MySQL-specific connection string
- `TESTDOCK_DSN_MONGODB` - MongoDB-specific connection string
- `TESTDOCK_DSN_<DRIVER_NAME>` - Custom connection string for a specific driver

### Retry and Connection Handling

- `WithRetryTimeout(duration)`: Configure connection retry timeout (default 3s). Must be less than totalRetryDuration
- `WithTotalRetryDuration(duration)`: Configure total retry duration (default 30s). Must be greater than retryTimeout
- `WithCloseTimeout(duration)`: Configure cleanup timeout for closing returned resources (default 30s). Must be greater than 0. It covers `pgxpool.Pool.Close`, `sql.DB.Close`, and `mongo.Client.Disconnect`. It does not cover SQL `DROP DATABASE`, MongoDB `Drop`, or Docker cleanup.

`WithRetryTimeout` and `WithTotalRetryDuration` also control retries for PostgreSQL `SQLSTATE 53300` while a migration connection is being established. Migration execution itself is never retried.

### Docker Configuration

- `WithDockerSocketEndpoint(endpoint)`: Custom Docker daemon socket
- `WithDockerPort(port)`: Override container port mapping
- `WithUnsetProxyEnv(bool)`: Unset proxy environment variables

If close timeout is reached, the test fails and later cleanup functions continue. A timeout usually means the test leaked a connection: `Rows` was not closed, `QueryRow` was used without `Scan`, or a transaction was not finished.

### Database Options

- `WithConnectDatabase(name)`: Override connection database
- `WithPrepareCleanUp(func)`: Custom cleanup handlers. The default is empty, but `GetPgxPool` and `GetPqConn` functions use it to automatically apply cleanup handlers to disconnect all users from the database before cleaning up.
- `WithLogger(logger)`: Custom logging implementation

### Default connection strings

- `DefaultPostgresDSN`: Default PostgreSQL connection string
- `DefaultMySQLDSN`: Default MySQL connection string
- `DefaultMongoDSN`: Default MongoDB connection string

## Migrations

TestDock supports two popular migration tools:

### Goose Migrations (SQL databases only)

<https://github.com/pressly/goose>

Parallel tests must not use Goose package-level state APIs such as `goose.SetDialect`, `goose.SetBaseFS`, `goose.Up*`, or `goose.Down*`. Use `WithMigrations`, `WithMigrationsToVersion`, `ApplyMigrations`, or `ApplyMigrationsToVersion`. When a rollback is required, create a separate `goose.Provider` for each temporary database. Close it before the migration helper returns instead of using `testing.TB.Cleanup`, and preserve migration and close errors with `errors.Join`.

```go
 db, _ := GetPqConn(t,
    "postgres://postgres:secret@127.0.0.1:5432/postgres?sslmode=disable",
    testdock.WithMigrations("migrations/pg/goose", testdock.GooseMigrateFactoryPQ),
    testdock.WithDockerImage("17.2"),
 )
```

### Golang-Migrate Migrations (SQL databases and MongoDB)

<https://github.com/golang-migrate/migrate>

```go
db, _ := GetMongoDatabase(t,
    testdock.DefaultMongoDSN,
    testdock.WithDockerRepository("mongo"),
    testdock.WithDockerImage("6.0.20"),
    testdock.WithMigrations("migrations/mongodb", testdock.GolangMigrateFactory),
 )
```

### Custom Migrations

You can also use a custom migration tool implementing the `testdock.MigrateFactory` interface.

## Requirements

- Go 1.26 or higher
- Docker (when using `RunModeDocker` or `RunModeAuto`)

## License

MIT License - see LICENSE for details

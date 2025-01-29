[![Go Reference](https://pkg.go.dev/badge/github.com/n-r-w/testdock.svg)](https://pkg.go.dev/github.com/n-r-w/testdock/v2)
[![Go Coverage](https://github.com/n-r-w/testdock/wiki/coverage.svg)](https://raw.githack.com/wiki/n-r-w/testdock/coverage.html)
![CI Status](https://github.com/n-r-w/testdock/actions/workflows/go.yml/badge.svg)

# TestDock

TestDock is a Go library that simplifies database testing by providing an easy way to create and manage test databases in realistic scenarios, instead of using mocks. It supports running tests against both Docker containers and external databases, with built-in support for MongoDB and various SQL databases.

## Features

- **Multiple Database Support**  
  - MongoDB: `GetMongoDatabase` function
  - PostgreSQL (with both `pgx` and `pq` drivers): `GetPgxPool` and `GetPqConn` functions
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

### PostgreSQL Example (using pgx)

```go
import (
    "testing"
    "github.com/n-r-w/testdock/v2"
)

func TestDatabase(t *testing.T) {          
    // Get a connection pool to a test database.

    /* 
    If the environment variable TESTDOCK_DSN_POSTGRES is set, then the input 
    connection string is ignored and the value from the environment variable
    is used. If the environment variable TESTDOCK_DSN_POSTGRES is not set, 
    then the input connection string is used to generate the Docker container 
    configuration.
    */

    pool, _ := testdock.GetPgxPool(t, 
        testdock.DefaultPostgresDSN,
        testdock.WithMigrations("migrations", testdock.GooseMigrateFactoryPGX),        
    )
    
    // Use the pool for your tests
    // The database will be automatically cleaned up after the test
}
```

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

### Docker Configuration

- `WithDockerSocketEndpoint(endpoint)`: Custom Docker daemon socket
- `WithDockerPort(port)`: Override container port mapping
- `WithUnsetProxyEnv(bool)`: Unset proxy environment variables

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
    WithDockerRepository("mongo"),
    WithDockerImage("6.0.20"),
    WithMigrations("migrations/mongodb", testdock.GolangMigrateFactory),
 )
```

### Custom Migrations

You can also use a custom migration tool implementing the `testdock.MigrateFactory` interface.

## Requirements

- Go 1.23 or higher
- Docker (when using `RunModeDocker` or `RunModeAuto`)

## License

MIT License - see LICENSE for details

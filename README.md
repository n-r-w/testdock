[![Go Reference](https://pkg.go.dev/badge/github.com/n-r-w/pglive.svg)](https://pkg.go.dev/github.com/n-r-w/pglive)
[![Go Coverage](https://github.com/n-r-w/pglive/wiki/coverage.svg)](https://raw.githack.com/wiki/n-r-w/pglive/coverage.html)
![CI Status](https://github.com/n-r-w/pglive/actions/workflows/go.yml/badge.svg)
[![Stability](http://badges.github.io/stability-badges/dist/stable.svg)](http://github.com/badges/stability-badges)
[![Go Report](https://goreportcard.com/badge/github.com/n-r-w/pglive?v=6b996d51d6235dbae980d0120d11be6ffd65851f)](https://goreportcard.com/badge/github.com/n-r-w/pglive)

# pglive

PostgreSQL live database unit testing

## Installation

```bash
go get github.com/n-r-w/pglive@latest
```

## Introduction

pglive is a Go package designed to simplify PostgreSQL database testing by providing:

- Isolated test databases for parallel test execution
- Automatic database creation and cleanup
- Flexible configuration options for both Docker and external database setups
- Support for multiple migration tools (gomigrate, goose) with custom migration support
- Comprehensive connection pooling configuration
- Detailed logging capabilities

## Features

- **Parallel Test Support**: Each test runs in its own isolated database instance
- **Automatic Cleanup**: Databases are automatically created and removed
- **Migration Support**:
  - Built-in support for gomigrate and goose
  - Custom migration tool integration via WithMigratorFactory
- **Connection Management**:
  - Configurable connection pooling
  - Connection health checks
  - Timeout and lifetime settings
- **Logging**:
  - Built-in structured logging
  - Custom logger support
- **Configuration Options**:
  - Docker and external database support
  - Environment variable and direct parameter configuration
  - GitLab CI integration

There are two operating modes: using Docker and using an external database.

### Using Docker

In this case, the default mapping for PostgreSQL on the host is to port 5433 to avoid conflicts with local PostgreSQL.
For Docker Desktop on Linux or macOS, you should define `DOCKER_SOCKET_ENDPOINT` environment variable with:

- Linux: `unix:///home/<user>/.docker/desktop/docker.sock`
- macOS: `unix:///Users/<USER>/.docker/run/docker.sock`

### Using an external database

This mode is activated in the following cases:

- at least one parameter in Option are used: WithHost, WithPort, WithDatabase, WithUser, WithPassword
- at least one environment variable is set: POSTGRES_HOST, POSTGRES_PORT, POSTGRES_DB, POSTGRES_USER, POSTGRES_PASSWORD.

Can be activated in GitLab CI with the following settings in .gitlab-ci.yml. Setting POSTGRES_DB in GitLab environment variables doesn't make sense because the default database won't be used. Example of .gitlab-ci.yml file with PostgreSQL service and environment variables for go tests:

```yaml
go tests:
  services:
    - name: postgres:16.2 # PostgreSQL image name from <https://hub.docker.com/_/postgres>
      alias: test-postgres
      command: ["-c", "max_connections=200"] # Increasing the number of connections (if you have a lot of parallel tests)
  variables: # Environment variables
    POSTGRES_USER: postgres # Username
    POSTGRES_PASSWORD: secret # Password
    POSTGRES_HOST: test-postgres # Hostname
    POSTGRES_HOST_AUTH_METHOD: trust
    GOFLAGS: # Flags for go test if needed (-tags=xxxx)
```

Known limitations in external database mode:

- The test database will not be deleted if the test execution is stopped on a breakpoint and not continued.

### Parameter filling priority

- If specific value parameters are set (Host, Port, Database, User, Password), the values from these parameters are used.
- If environment variables are set and specific value parameters are not set, the values from the environment variables are used.
- If neither environment variables nor specific value parameters are set, default values are used.

Default values:

```plaintext
PostrgreSQL image: postgres:latest
PostrgreSQL Host: 127.0.0.1
PostrgreSQL port: 5432
PostrgreSQL mapping port: 5433 (Docker mode)
User: postgres
Password: secret
```

## Usage example

### Basic Usage

```go
import (
  "testing"
  "context"

  "github.com/jackc/pgx/v5/pgxpool"
  "github.com/n-r-w/pglive"
)

func Test_Example(t *testing.T) {
    t.Parallel()
    
    // Create test database, run migrations and return a database connection
    db := pglive.GetPool(t, "./migrations")
    defer db.Close()

    // Example query
    rows, err := db.Query(context.Background(), "SELECT name FROM test_table")
    if err != nil {
        t.Fatalf("error: %s", err)
    }
    defer rows.Close()
    
    for rows.Next() {
        var name string
        if err := rows.Scan(&name); err != nil {
            t.Fatalf("error: %s", err)
        }
        if name != "test" {
            t.Fatalf("expected 'test', got '%s'", name)
        }
    }
}
```

### Using Specific Migration Tool

```go
import (
  "testing"
  "time"

  "github.com/jackc/pgx/v5/pgxpool"
  "github.com/n-r-w/pglive"
)

func Test_WithGooseMigrations(t *testing.T) {
    t.Parallel()
    
    db := pglive.GetPool(t, "./migrations/goose", 
        pglive.WithMigratorFactory(pglive.GooseMigratorFactory),        
    )
    defer db.Close()
    
    // Test code...
}

func Test_WithGoMigrateMigrations(t *testing.T) {
    t.Parallel()
    
    db := pglive.GetPool(t, "./migrations/gomigrate", 
        pglive.WithMigratorFactory(pglive.GolangMigrateFactory),        
    )
    defer db.Close()
    
    // Test code...
}

[![Go Reference](https://pkg.go.dev/badge/github.com/n-r-w/pglive.svg)](https://pkg.go.dev/github.com/n-r-w/pglive)
[![Go Coverage](https://github.com/n-r-w/pglive/wiki/coverage.svg)](https://raw.githack.com/wiki/n-r-w/pglive/coverage.html)
![CI Status](https://github.com/n-r-w/pglive/actions/workflows/go.yml/badge.svg)
[![Stability](http://badges.github.io/stability-badges/dist/stable.svg)](http://github.com/badges/stability-badges)
[![Go Report](https://goreportcard.com/badge/github.com/n-r-w/pglive)](https://goreportcard.com/badge/github.com/n-r-w/pglive?v=6b996d51d6235dbae980d0120d11be6ffd65851f)

# pglive

PostgreSQL live database unit testing

## Installation

```bash
go get github.com/n-r-w/pglive@latest
```

## Introduction

- This package is used with <https://github.com/jackc/pgx>, because it returns pgxpool.Pool for database operations.
- Allow to use parallel unit tests. In this case, each test will have its own database.
- Test database is created and deleted automatically after the test is finished.

There are two operating modes: using Docker and using an external database.

### Using Docker

In this case, the default mapping for PostgreSQL on the host is to port 5433 to avoid conflicts with local PostgreSQL.

Known limitations in Docker mode:

- In docker mode, this package uses <https://github.com/ory/dockertest>. According to [Cannot find /var/run/docker.sock on Mac/Windows](https://github.com/ory/dockertest/issues/413), dockertest does not support macOS, Windows and Docker Desktop. It is recommended to use the external database mode in these cases.
- The docker container will be deleted after the test execution is stopped on a breakpoint and not continued.

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

```
PostrgreSQL image: postgres:latest
PostrgreSQL Host: 127.0.0.1
PostrgreSQL port: 5432
PostrgreSQL mapping port: 5433 (Docker mode)
User: postgres
Password: secret
```

## Usage example

```go
import (
  "testing"

  "github.com/jackc/pgx/v5/pgxpool"
  "github.com/n-r-w/pglive"
)

func Test_Example(t *testing.T) {
    var db *pgxpool.Pool

    // Create test database, run migrations and return a database connection
    db = pglive.GetPool(t, "./migrations")

    rows, err := db.Query(context.Background(), "SELECT name FROM test_table")
    if err != nil {
        t.Fatalf("error: %s", err)
    }
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

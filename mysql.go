package testdock

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql" // mysql driver
)

// GetMySQLConn inits a test mysql database, applies migrations.
// Use user root for docker test database.
func GetMySQLConn(tb testing.TB, dsn string, opt ...Option) *sql.DB {
	tb.Helper()

	url, err := parseURL(dsn)
	if err != nil {
		tb.Fatalf("failed to parse dsn: %v", err)
	}

	optPrepared := make([]Option, 0, len(opt))

	optPrepared = append(optPrepared,
		WithDockerRepository("mysql"),
		WithDockerImage("9.1.0"),
		WithRetryTimeout(time.Second*60), //nolint:mnd // 30s not enough
		WithDockerEnv([]string{
			fmt.Sprintf("MYSQL_ROOT_PASSWORD=%s", url.Password),
			fmt.Sprintf("MYSQL_DATABASE=%s", url.Database),
		}),
	)

	optPrepared = append(optPrepared, opt...)

	return GetSQLConn(tb, "mysql", dsn, optPrepared...)
}

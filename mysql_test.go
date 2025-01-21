package testdock

import (
	"database/sql"
	"testing"
	"time"
)

func Test_MySQLDB(t *testing.T) {
	t.Parallel()

	db := GetMySQLConn(t,
		DefaultMySQLDSN,
		WithMigrations("migrations/pg/goose", GooseMigrateFactoryMySQL),
		WithRetryTimeout(time.Second*5),
		WithTotalRetryDuration(time.Second*60), //nolint:mnd // for Docker 30s not enough
	)

	testSQLHelper(t, db)
}

func testSQLHelper(t *testing.T, db *sql.DB) {
	t.Helper()

	rows, err := db.Query("SELECT name FROM test_table")
	if err != nil {
		t.Fatalf("error: %s", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("error: %s", err)
		}
		if name != "test" { //nolint:goconst // ok
			t.Fatalf("expected 'test', got '%s'", name)
		}
	}
}

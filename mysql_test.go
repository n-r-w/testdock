package testdock

import (
	"database/sql"
	"testing"

	"github.com/pressly/goose/v3"
)

func Test_MySQLDB(t *testing.T) {
	t.Parallel()

	db := GetMysqlConn(t,
		DefaultMysqlDSN,
		WithMigrations("migrations/pg/goose", GooseMigrateFactory(goose.DialectMySQL, "mysql")),
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

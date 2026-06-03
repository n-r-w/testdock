package testdock

import (
	"database/sql"
	"testing"
	"time"
)

func Test_MySQLDB(t *testing.T) {
	t.Parallel()

	db, informer := GetMySQLConn(t,
		DefaultMySQLDSN,
		WithMigrations("migrations/pg/goose", GooseMigrateFactoryMySQL),
		WithRetryTimeout(time.Second*5),
		WithTotalRetryDuration(time.Second*60),
	)

	checkInformer(t, DefaultMySQLDSN, informer)

	testSQLHelper(t, db)
}

func testSQLHelper(t *testing.T, db *sql.DB) {
	t.Helper()

	rows, err := db.QueryContext(t.Context(), "SELECT name FROM test_table")
	if err != nil {
		t.Fatalf("error: %s", err)
	}
	t.Cleanup(func() { _ = rows.Close() })
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

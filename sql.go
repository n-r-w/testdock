package testdock

import (
	"database/sql"
	"fmt"
	"testing"
)

// GetSQLConn inits a test database, applies migrations, and returns sql connection to the database.
// driver: https://go.dev/wiki/SQLDrivers.
// Do not forget to import corresponding driver package.
func GetSQLConn(tb testing.TB, driver, dsn string, opt ...Option) *sql.DB {
	tb.Helper()

	tDB := newTDB(tb, driver, dsn, opt)

	db, err := tDB.connectSQLDB(true)
	if err != nil {
		tDB.logger.Fatalf("%v", err)
	}

	tb.Cleanup(func() { _ = db.Close() })

	return db
}

// connectSQLDB connects to the database with retries using database/sql.
// testDatabase: if true, will be connected to the temporary test database.
func (d *testDB) connectSQLDB(testDatabase bool) (*sql.DB, error) {
	var dbURL *dbURL
	if testDatabase {
		dbURL = d.url.replaceDatabase(d.databaseName)
	} else {
		dbURL = d.url.replaceDatabase(d.connectDatabase)
	}

	d.logger.Logf("[%s] connecting to test database", dbURL.string(true))

	var db *sql.DB
	err := d.retryConnect(dbURL.string(true), func() (err error) {
		db, err = sql.Open(d.driver, dbURL.string(false))
		if err != nil {
			return err
		}
		if err = db.Ping(); err != nil {
			_ = db.Close()
			return err
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("connect url (%s): %w", dbURL.string(false), err)
	}

	return db, nil
}

func (d *testDB) createSQLDatabase() error {
	d.logger.Logf("[%s] creating new test sql dastabase %s", d.dsnNoPass, d.databaseName)

	db, err := d.connectSQLDB(false)
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.Exec(fmt.Sprintf("CREATE DATABASE %s", d.databaseName))
	if err != nil {
		return fmt.Errorf("create db: %w", err)
	}

	d.logger.Logf("[%s] new test sql dastabase %s created", d.dsnNoPass, d.databaseName)

	return nil
}

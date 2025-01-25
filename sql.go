package testdock

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
)

// GetSQLConn inits a test database, applies migrations, and returns sql connection to the database.
// driver: https://go.dev/wiki/SQLDrivers.
// Do not forget to import corresponding driver package.
func GetSQLConn(tb testing.TB, driver, dsn string, opt ...Option) (*sql.DB, Informer) {
	tb.Helper()

	ctx := context.Background()
	tDB := newTDB(ctx, tb, driver, dsn, opt)

	db, err := tDB.connectSQLDB(ctx, true)
	if err != nil {
		tb.Fatalf("cannot connect to database: %v", err)
	}

	tb.Cleanup(func() { _ = db.Close() })

	return db, tDB
}

// connectSQLDB connects to the database with retries using database/sql.
// testDatabase: if true, will be connected to the temporary test database.
func (d *testDB) connectSQLDB(ctx context.Context, testDatabase bool) (*sql.DB, error) {
	var dbURL *dbURL
	if testDatabase {
		dbURL = d.url.replaceDatabase(d.databaseName)
	} else {
		dbURL = d.url.replaceDatabase(d.connectDatabase)
	}

	d.logger.Info(ctx, "connecting to test database", "url", dbURL.string(true))

	var db *sql.DB
	err := d.retryConnect(ctx, dbURL.string(true), func() (err error) {
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

func (d *testDB) createSQLDatabase(ctx context.Context) error {
	d.logger.Info(ctx, "creating new test sql database", "dsn", d.dsnNoPass, "database", d.databaseName)

	db, err := d.connectSQLDB(ctx, false)
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.Exec(fmt.Sprintf("CREATE DATABASE %s", d.databaseName))
	if err != nil {
		return fmt.Errorf("create db: %w", err)
	}

	d.logger.Info(ctx, "new test sql database created", "dsn", d.dsnNoPass, "database", d.databaseName)

	return nil
}

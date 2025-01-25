package testdock

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx postgres driver
	_ "github.com/lib/pq"              // pq postgres driver
)

// GetPgxPool inits a test postgresql (pgx driver) database, applies migrations,
// and returns pgx connection pool to the database.
func GetPgxPool(tb testing.TB, dsn string, opt ...Option) (*pgxpool.Pool, Informer) {
	tb.Helper()

	ctx := context.Background()

	tDB := newTDB(ctx, tb, "pgx", dsn, getPostgresOptions(tb, dsn, opt...))

	db, err := tDB.connectPgxDB(ctx)
	if err != nil {
		tb.Fatalf("cannot connect to postgres: %v", err)
	}

	tb.Cleanup(func() { db.Close() })

	return db, tDB
}

// GetPqConn inits a test postgresql (pq driver) database, applies migrations,
// and returns sql connection to the database.
func GetPqConn(ctx context.Context, tb testing.TB, dsn string, opt ...Option) (*sql.DB, Informer) {
	tb.Helper()

	tDB := newTDB(ctx, tb, "postgres", dsn, getPostgresOptions(tb, dsn, opt...))

	db, err := tDB.connectSQLDB(ctx, true)
	if err != nil {
		tb.Fatalf("cannot connect to postgres: %v", err)
	}

	tb.Cleanup(func() { _ = db.Close() })

	return db, tDB
}

// connectPgxDB connects to the database with retries using pgx.
func (d *testDB) connectPgxDB(ctx context.Context) (*pgxpool.Pool, error) {
	var db *pgxpool.Pool
	dbURL := d.url.replaceDatabase(d.databaseName)
	d.logger.Info(ctx, "connecting to test database", "url", dbURL.string(true))

	err := d.retryConnect(ctx, dbURL.string(true), func() (err error) {
		db, err = pgxpool.New(ctx, dbURL.string(false))
		if err != nil {
			return err
		}
		if err = db.Ping(ctx); err != nil {
			db.Close()
			return err
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("connect postgres url (%s): %w", dbURL.string(false), err)
	}

	return db, nil
}

// disconnect users before deleting the database
func disconnectUsers(db *sql.DB, databaseName string) error {
	_, err := db.Exec(
		`SELECT pg_terminate_backend(pg_stat_activity.pid) 
				FROM pg_stat_activity 
				WHERE datname = $1 AND pid <> pg_backend_pid()`,
		databaseName)
	return err
}

// getPostgresOptions returns the options for the postgresql database.
func getPostgresOptions(tb testing.TB, dsn string, opt ...Option) []Option {
	tb.Helper()

	url, err := parseURL(dsn)
	if err != nil {
		tb.Fatalf("failed to parse dsn: %v", err)
	}

	optPrepared := make([]Option, 0, len(opt))
	optPrepared = append(optPrepared,
		WithDockerRepository("postgres"),
		WithPrepareCleanUp(disconnectUsers),
		WithDockerEnv([]string{
			fmt.Sprintf("POSTGRES_USER=%s", url.User),
			fmt.Sprintf("POSTGRES_PASSWORD=%s", url.Password),
			fmt.Sprintf("POSTGRES_DB=%s", url.Database),
			"listen_addresses = '*'",
			"max_connections = 1000",
		}),
	)

	optPrepared = append(optPrepared, opt...)

	return optPrepared
}

package testdock

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresTemplate owns a prepared PostgreSQL source for cloned test databases.
// The testing.TB passed to NewPostgresTemplate controls the source lifetime.
type PostgresTemplate struct {
	source *testDB // immutable source whose testing.TB also owns shared Docker infrastructure
}

// PostgresTemplateOption configures one-time source database preparation.
type PostgresTemplateOption func(*postgresTemplateConfig)

// PostgresTemplateSetup prepares source data after automatic migrations finish.
type PostgresTemplateSetup func(testing.TB, *pgxpool.Pool, Informer)

// postgresTemplateConfig separates source preparation from per-clone database options.
type postgresTemplateConfig struct {
	databaseOptions []Option              // options executed only while creating the source database
	setup           PostgresTemplateSetup // optional state preparation copied into every clone
}

// WithPostgresTemplateOptions applies database options only while preparing the source database.
// Cloned databases inherit the resulting state and do not execute these options again.
func WithPostgresTemplateOptions(options ...Option) PostgresTemplateOption {
	return func(config *postgresTemplateConfig) {
		config.databaseOptions = append(config.databaseOptions, options...)
	}
}

// WithPostgresTemplateSetup registers one-time source preparation after automatic migrations.
func WithPostgresTemplateSetup(setup PostgresTemplateSetup) PostgresTemplateOption {
	return func(config *postgresTemplateConfig) {
		config.setup = setup
	}
}

// NewPostgresTemplate prepares a parent-owned PostgreSQL source database and closes every source
// connection before allowing clones. Source cleanup runs after the parent and all its subtests end.
func NewPostgresTemplate(
	tb testing.TB,
	dsn string,
	options ...PostgresTemplateOption,
) *PostgresTemplate {
	tb.Helper()

	// Resolve template-only options before creating the source database.
	config := postgresTemplateConfig{
		databaseOptions: nil,
		setup:           nil,
	}
	for _, option := range options {
		option(&config)
	}

	// Reuse the standard PostgreSQL lifecycle so Docker and external modes behave consistently.
	ctx := context.Background()
	source := newTDB(ctx, tb, pgxDriverName, dsn, getPostgresOptions(tb, dsn, config.databaseOptions...))

	// The setup pool is never exposed because PostgreSQL requires a connection-free source database.
	setupPool, err := source.connectPgxDB(ctx)
	if err != nil {
		tb.Fatalf("cannot connect to postgres template: %v", err)
	}
	defer func() {
		if closeErr := closeResourceWithTimeout(source.closeTimeout, func() error {
			setupPool.Close()
			return nil
		}, func() string {
			return source.closeTimeoutDetails("postgres template pgxpool", snapshotPgxPoolStats(setupPool))
		}); closeErr != nil {
			tb.Fatalf("cannot close postgres template pool: %v", closeErr)
		}
	}()

	// Additional source state is created once and becomes part of every clone.
	if config.setup != nil {
		config.setup(tb, setupPool, source)
	}

	return &PostgresTemplate{source: source}
}

// GetPgxPool creates an isolated physical clone and returns a pool owned by the child test.
func (template *PostgresTemplate) GetPgxPool(tb testing.TB) (*pgxpool.Pool, Informer) {
	tb.Helper()

	// The parent source keeps Docker infrastructure alive, so clones use its resolved server as an
	// external PostgreSQL instance and drop their own databases during child cleanup.
	resolvedDSN := template.source.url.string(false)
	cloneOptions := getPostgresOptions(tb, resolvedDSN,
		WithMode(RunModeExternal),
		WithConnectDatabase(template.source.connectDatabase),
		WithRetryTimeout(template.source.retryTimeout),
		WithTotalRetryDuration(template.source.totalRetryDuration),
		WithCloseTimeout(template.source.closeTimeout),
		withDatabaseTemplate(template.source.databaseName),
	)

	ctx := context.Background()
	clone := newTDB(ctx, tb, pgxDriverName, resolvedDSN, cloneOptions)
	pool, err := clone.connectPgxDB(ctx)
	if err != nil {
		tb.Fatalf("cannot connect to postgres clone: %v", err)
	}

	// Close the child pool before the database cleanup registered by newTDB drops the clone.
	tb.Cleanup(func() {
		if closeErr := closeResourceWithTimeout(clone.closeTimeout, func() error {
			pool.Close()
			return nil
		}, func() string {
			return clone.closeTimeoutDetails("pgxpool", snapshotPgxPoolStats(pool))
		}); closeErr != nil {
			tb.Errorf("%v", closeErr)
		}
	})

	return pool, clone
}

// withDatabaseTemplate selects the immutable PostgreSQL source copied by CREATE DATABASE.
func withDatabaseTemplate(databaseName string) Option {
	return func(database *testDB) {
		database.databaseTemplate = databaseName
	}
}

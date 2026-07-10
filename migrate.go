package testdock

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/mongodb"  // require for mongodb
	_ "github.com/golang-migrate/migrate/v4/database/postgres" // require for gomigrate
	_ "github.com/golang-migrate/migrate/v4/source/file"       // require for gomigrate
	"github.com/n-r-w/ctxlog"
	"github.com/pressly/goose/v3"
)

// MigrateFactory creates a new migrator.
type MigrateFactory func(t testing.TB, dsn, migrationsDir string, logger ctxlog.ILogger) (Migrator, error)

// Migrator interface for applying migrations.
type Migrator interface {
	Up(ctx context.Context) error
}

// VersionedMigrator is the contract for migration factories used with WithMigrationsToVersion.
// The version is the numeric file prefix before "_", including timestamp prefixes.
type VersionedMigrator interface {
	Migrator
	UpTo(ctx context.Context, version int64) error
}

// migrationConnectionPreparer exposes connection setup for built-in lazy migrators without changing public APIs.
type migrationConnectionPreparer interface {
	prepareMigrationConnection(ctx context.Context) error
	closeMigrationConnection() error
}

// migrationRunConfig defines retry and target-version behavior for one migration execution.
type migrationRunConfig struct {
	retryTimeout       time.Duration // delay between SQLSTATE 53300 connection attempts
	totalRetryDuration time.Duration // maximum connection preparation duration
	targetVersion      int64         // numeric migration prefix where execution stops
	hasTargetVersion   bool          // enables version-limited migration execution
}

// ApplyMigrations applies all pending migrations to an existing test database.
// The helper fails tb on invalid input, migrator creation errors, or migration errors.
func ApplyMigrations(tb testing.TB, dsn, migrationsDir string, migrateFactory MigrateFactory) {
	tb.Helper()

	logger := ctxlog.Must(ctxlog.WithTesting(tb))
	err := runMigrations(tb.Context(), tb, dsn, migrationsDir, migrateFactory, logger, migrationRunConfig{
		retryTimeout:       DefaultRetryTimeout,
		totalRetryDuration: DefaultTotalRetryDuration,
		targetVersion:      0,
		hasTargetVersion:   false,
	})
	if err != nil {
		tb.Fatalf("cannot apply migrations: %v", err)
	}
}

// ApplyMigrationsToVersion applies pending migrations up to and including the target version.
// The version is the numeric file prefix before "_", including timestamp prefixes.
// Custom factories must return a migrator that implements VersionedMigrator.
func ApplyMigrationsToVersion(tb testing.TB, dsn, migrationsDir string, migrateFactory MigrateFactory, version int64) {
	tb.Helper()

	logger := ctxlog.Must(ctxlog.WithTesting(tb))
	err := runMigrations(tb.Context(), tb, dsn, migrationsDir, migrateFactory, logger, migrationRunConfig{
		retryTimeout:       DefaultRetryTimeout,
		totalRetryDuration: DefaultTotalRetryDuration,
		targetVersion:      version,
		hasTargetVersion:   true,
	})
	if err != nil {
		tb.Fatalf("cannot apply migrations to version: %v", err)
	}
}

// runMigrations prepares one migrator connection with retry and executes its migration body once.
func runMigrations(
	ctx context.Context,
	tb testing.TB,
	dsn, migrationsDir string,
	migrateFactory MigrateFactory,
	logger ctxlog.ILogger,
	config migrationRunConfig,
) error {
	if dsn == "" {
		return errors.New("dsn is empty")
	}
	if migrationsDir == "" {
		return errors.New("migrationsDir is empty")
	}
	if migrateFactory == nil {
		return errors.New("migrateFactory is nil")
	}
	if config.hasTargetVersion {
		if err := validateMigrationVersion(config.targetVersion); err != nil {
			return err
		}
	}

	// Retry eager connection setup performed by migration factories before a migrator is returned.
	var migrator Migrator
	err := retryPostgresOperation(
		ctx,
		logger,
		config.retryTimeout,
		config.totalRetryDuration,
		"create migrator",
		func() error {
			candidate, factoryErr := migrateFactory(tb, dsn, migrationsDir, logger)
			if factoryErr == nil {
				migrator = candidate
			}
			return factoryErr
		},
	)
	if err != nil {
		return fmt.Errorf("new migrator: %w", err)
	}
	if migrator == nil {
		return errors.New("migrateFactory returned nil migrator")
	}

	// Goose and other built-in lazy migrators reserve a connection before their migration body starts.
	if preparer, ok := migrator.(migrationConnectionPreparer); ok {
		if err = retryPostgresOperation(
			ctx,
			logger,
			config.retryTimeout,
			config.totalRetryDuration,
			"prepare migration connection",
			func() error { return preparer.prepareMigrationConnection(ctx) },
		); err != nil {
			closeErr := preparer.closeMigrationConnection()
			if closeErr != nil {
				closeErr = fmt.Errorf("close migrator after connection failure: %w", closeErr)
			}
			return errors.Join(fmt.Errorf("prepare migration connection: %w", err), closeErr)
		}
	}

	if config.hasTargetVersion {
		if err = migrateUpToVersion(ctx, migrator, config.targetVersion); err != nil {
			return fmt.Errorf("up migrations to version: %w", err)
		}
		return nil
	}

	if err = migrator.Up(ctx); err != nil {
		return fmt.Errorf("up migrations: %w", err)
	}
	return nil
}

// migrateUpToVersion applies migrations up to the numeric file prefix requested by the test.
func migrateUpToVersion(ctx context.Context, migrator Migrator, version int64) error {
	if err := validateMigrationVersion(version); err != nil {
		return err
	}

	versionedMigrator, ok := migrator.(VersionedMigrator)
	if !ok {
		return errors.New("WithMigrationsToVersion and ApplyMigrationsToVersion require " +
			"migrator to implement VersionedMigrator")
	}

	return versionedMigrator.UpTo(ctx, version)
}

// validateMigrationVersion rejects values that cannot match a migration file prefix.
func validateMigrationVersion(version int64) error {
	if version <= 0 {
		return errors.New("migration version must be greater than 0")
	}

	return nil
}

//nolint:gochecknoglobals // predefined migrator factories.
var (
	// GooseMigrateFactoryPGX is a migrator for https://github.com/pressly/goose with pgx driver.
	GooseMigrateFactoryPGX = GooseMigrateFactory(goose.DialectPostgres, pgxDriverName)
	// GooseMigrateFactoryPQ is a migrator for https://github.com/pressly/goose with pq driver.
	GooseMigrateFactoryPQ = GooseMigrateFactory(goose.DialectPostgres, postgresDriverName)
	// GooseMigrateFactoryMySQL is a migrator for https://github.com/pressly/goose with mysql driver.
	GooseMigrateFactoryMySQL = GooseMigrateFactory(goose.DialectMySQL, "mysql")
)

// GooseMigrateFactory creates a new migrator for https://github.com/pressly/goose.
func GooseMigrateFactory(dialect goose.Dialect, driver string) MigrateFactory {
	return func(t testing.TB, dsn, migrationsDir string, logger ctxlog.ILogger) (Migrator, error) {
		return newGooseMigrator(t, dialect, driver, dsn, migrationsDir, logger)
	}
}

// gooseMigrator is a migrator for goose.
type gooseMigrator struct {
	db *sql.DB
	p  *goose.Provider
}

// newGooseMigrator creates a new migrator for goose.
func newGooseMigrator(
	t testing.TB,
	dialect goose.Dialect,
	driver, dsn, migrationsDir string,
	logger ctxlog.ILogger,
) (*gooseMigrator, error) {
	conn, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("sql open url (%s): %w", dsn, err)
	}

	p, err := goose.NewProvider(dialect, conn, os.DirFS(migrationsDir),
		goose.WithLogger(NewGooseLogger(t, logger)),
		goose.WithVerbose(true),
	)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("new goose provider: %w", err)
	}

	return &gooseMigrator{
		db: conn,
		p:  p,
	}, nil
}

// prepareMigrationConnection establishes Goose's lazy database connection before migrations start.
func (m *gooseMigrator) prepareMigrationConnection(ctx context.Context) error {
	return m.db.PingContext(ctx)
}

// closeMigrationConnection releases Goose resources when connection preparation cannot complete.
func (m *gooseMigrator) closeMigrationConnection() error {
	return m.p.Close()
}

func (m *gooseMigrator) Up(ctx context.Context) error {
	defer m.p.Close() //nolint:errcheck // Close only releases resources; keep migration result.

	_, err := m.p.Up(ctx)
	return err
}

// UpTo applies goose migrations up to and including the target numeric file prefix.
func (m *gooseMigrator) UpTo(ctx context.Context, version int64) error {
	defer m.p.Close() //nolint:errcheck // Close only releases resources; keep migration result.

	_, err := m.p.UpTo(ctx, version)
	return err
}

// GolangMigrateFactory creates a new migrator for https://github.com/golang-migrate/migrate.
func GolangMigrateFactory(_ testing.TB, dsn, migrationsDir string, logger ctxlog.ILogger) (Migrator, error) {
	return newGolangMigrateMigrator(dsn, migrationsDir, logger)
}

// golangMigrateMigrator is a migrator for https://github.com/golang-migrate/migrate.
type golangMigrateMigrator struct {
	m *migrate.Migrate
}

// newGolangMigrateMigrator creates a new migrator for https://github.com/golang-migrate/migrate.
func newGolangMigrateMigrator(dsn, migrationsDir string, logger ctxlog.ILogger) (*golangMigrateMigrator, error) {
	if !filepath.IsAbs(migrationsDir) {
		var err error
		migrationsDir, err = filepath.Abs(migrationsDir)
		if err != nil {
			return nil, fmt.Errorf("get absolute path: %w", err)
		}
	}

	m, err := migrate.New("file://"+migrationsDir, dsn)
	if err != nil {
		return nil, fmt.Errorf("new migrate: %w", err)
	}

	m.Log = NewGolangMigrateLogger(logger)

	return &golangMigrateMigrator{m: m}, nil
}

func (m *golangMigrateMigrator) Up(_ context.Context) error {
	return m.m.Up()
}

// UpTo applies golang-migrate migrations up to the target numeric file prefix.
func (m *golangMigrateMigrator) UpTo(_ context.Context, version int64) error {
	migrationVersion, err := migrationVersionToUint(version)
	if err != nil {
		return err
	}

	return m.m.Migrate(migrationVersion)
}

// migrationVersionToUint validates that the public int64 version fits golang-migrate.
func migrationVersionToUint(version int64) (uint, error) {
	if err := validateMigrationVersion(version); err != nil {
		return 0, err
	}
	const maxUint32 = int64(1<<32 - 1)
	if strconv.IntSize == 32 && version > maxUint32 {
		return 0, fmt.Errorf("migration version %d overflows uint", version)
	}

	//nolint:gosec // version is positive, and overflow is checked above on 32-bit platforms.
	return uint(version), nil
}

// GooseLogger is a logger for goose.
type GooseLogger struct {
	t testing.TB
	l ctxlog.ILogger
}

// NewGooseLogger creates a new goose logger.
func NewGooseLogger(t testing.TB, l ctxlog.ILogger) *GooseLogger {
	return &GooseLogger{t: t, l: l}
}

// Fatalf logs a fatal error.
func (l GooseLogger) Fatalf(format string, v ...any) {
	l.t.Fatalf(format, v...)
}

// Printf logs a message.
func (l GooseLogger) Printf(format string, v ...any) {
	l.l.Info(context.Background(), fmt.Sprintf(format, v...))
}

// GolangMigrateLogger is a logger for golang-migrate.
type GolangMigrateLogger struct {
	l ctxlog.ILogger
}

// NewGolangMigrateLogger creates a new golang-migrate logger.
func NewGolangMigrateLogger(l ctxlog.ILogger) *GolangMigrateLogger {
	return &GolangMigrateLogger{l: l}
}

// Printf logs a message.
func (g *GolangMigrateLogger) Printf(format string, v ...any) {
	g.l.Info(context.Background(), fmt.Sprintf(format, v...))
}

// Verbose returns true.
func (*GolangMigrateLogger) Verbose() bool {
	return true
}

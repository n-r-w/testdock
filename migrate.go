package testdock

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/mongodb"  // require for mongodb
	_ "github.com/golang-migrate/migrate/v4/database/postgres" // require for gomigrate
	_ "github.com/golang-migrate/migrate/v4/source/file"       // require for gomigrate
	"github.com/n-r-w/ctxlog"
	"github.com/pressly/goose/v3"
)

// MigrateFactory creates a new migrator.
type MigrateFactory func(t testing.TB, dsn string, migrationsDir string, logger ctxlog.ILogger) (Migrator, error)

// Migrator interface for applying migrations.
type Migrator interface {
	Up(ctx context.Context) error
}

var (
	// GooseMigrateFactoryPGX is a migrator for https://github.com/pressly/goose with pgx driver.
	GooseMigrateFactoryPGX = GooseMigrateFactory(goose.DialectPostgres, "pgx")
	// GooseMigrateFactoryPQ is a migrator for https://github.com/pressly/goose with pq driver.
	GooseMigrateFactoryPQ = GooseMigrateFactory(goose.DialectPostgres, "postgres")
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
	p *goose.Provider
}

// newGooseMigrator creates a new migrator for goose.
func newGooseMigrator(t testing.TB, dialect goose.Dialect, driver, dsn, migrationsDir string, logger ctxlog.ILogger) (*gooseMigrator, error) {
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
		p: p,
	}, nil
}

func (m *gooseMigrator) Up(ctx context.Context) error {
	defer m.p.Close()

	_, err := m.p.Up(ctx)
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
func (g *GolangMigrateLogger) Verbose() bool {
	return true
}

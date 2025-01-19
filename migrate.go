package testdock

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/mongodb"  // require for mongodb
	_ "github.com/golang-migrate/migrate/v4/database/postgres" // require for gomigrate
	_ "github.com/golang-migrate/migrate/v4/source/file"       // require for gomigrate
	"github.com/pressly/goose/v3"
)

// MigrateFactory creates a new migrator.
type MigrateFactory func(dsn string, migrationsDir string, logger Logger) (Migrator, error)

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
	return func(dsn, migrationsDir string, logger Logger) (Migrator, error) {
		return newGooseMigrator(dialect, driver, dsn, migrationsDir, logger)
	}
}

// gooseMigrator is a migrator for goose.
type gooseMigrator struct {
	p *goose.Provider
}

// newGooseMigrator creates a new migrator for goose.
func newGooseMigrator(dialect goose.Dialect, driver, dsn, migrationsDir string, logger Logger) (*gooseMigrator, error) {
	conn, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("sql open url (%s): %w", dsn, err)
	}

	p, err := goose.NewProvider(dialect, conn, os.DirFS(migrationsDir),
		goose.WithLogger(NewGooseLogger(logger)),
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
func GolangMigrateFactory(dsn, migrationsDir string, logger Logger) (Migrator, error) {
	return newGolangMigrateMigrator(dsn, migrationsDir, logger)
}

// golangMigrateMigrator is a migrator for https://github.com/golang-migrate/migrate.
type golangMigrateMigrator struct {
	m *migrate.Migrate
}

// newGolangMigrateMigrator creates a new migrator for https://github.com/golang-migrate/migrate.
func newGolangMigrateMigrator(dsn, migrationsDir string, logger Logger) (*golangMigrateMigrator, error) {
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

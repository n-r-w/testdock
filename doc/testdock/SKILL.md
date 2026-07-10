---
name: testdock
description: Guidelines for using `github.com/n-r-w/testdock/v2` package.
metadata:
  version: "1.2"
---

<testdock name="github.com/n-r-w/testdock/v2 guidelines">
    <instructions>
        1. Use GetPgxPool, GetPqConn, GetMySQLConn, GetSQLConn, GetMongoDatabase, or GetMongoDatabaseV2 according to the database driver.
        2. Each Get... call creates a separate independent temporary database with a unique name.
        3. It is safe to call Get... from t.Parallel() tests; separate databases prevent database state conflicts between tests.
        4. Do not add manual cleanup for resources returned by Get...; testdock registers tb.Cleanup for database cleanup and connection closing. Down migrations are NOT REQUIRED - testdock automatically drops the temporary database after test completion.
        5. Use the returned Informer when the test needs the real DSN, Host, Port, or DatabaseName.
        6. RunModeAuto is the default: TESTDOCK_DSN_<DRIVER_NAME> selects an external database; otherwise testdock starts Docker.
        7. Use WithMode only when the test must force RunModeDocker or RunModeExternal.
        8. Use WithMigrations(dir, factory) to apply all migrations.
        9. Use WithMigrationsToVersion(dir, factory, version) to apply migrations only up to a target version.
        10. Use ApplyMigrations(t, dsn, dir, factory) to apply all pending migrations to an existing temporary database.
        11. Use ApplyMigrationsToVersion(t, dsn, dir, factory, version) to apply pending migrations up to and including version.
        12. Always pass migrationsDir and MigrateFactory together.
        13. Use GooseMigrateFactoryPGX, GooseMigrateFactoryPQ, GooseMigrateFactoryMySQL, GolangMigrateFactory, or a custom MigrateFactory.
        14. Use WithDockerRepository, WithDockerImage, WithDockerPort, WithDockerSocketEndpoint, WithDockerEnv, and WithUnsetProxyEnv only when default Docker settings are not enough.
        15. Use WithRetryTimeout and WithTotalRetryDuration only for slow startup or PostgreSQL SQLSTATE 53300 during migration connection setup; retry timeout must be less than total retry duration. Migration execution is never retried.
        16. Use WithCloseTimeout only for slow cleanup; close timeout must be greater than 0.
        17. Parallel tests MUST NOT use Goose package-level state APIs (goose.SetDialect, goose.SetBaseFS, goose.Up*, goose.Down*). Use WithMigrations, WithMigrationsToVersion, ApplyMigrations, or ApplyMigrationsToVersion instead.
            For rollback operations, create a separate goose.Provider for each temporary database.
        18. Tests that create a goose.Provider directly MUST call Provider.Close before the migration helper returns. MUST NOT register provider cleanup with tb.Cleanup because it retains a database connection. Preserve migration and close errors with errors.Join.
    </instructions>
    <examples>
        ```go
        import (
            "testing"
            "github.com/n-r-w/testdock/v2"
        )

        func TestRepository(t *testing.T) {
            t.Parallel()

            pool, _ := testdock.GetPgxPool(
                t,
                testdock.DefaultPostgresDSN,
                testdock.WithMigrations("{migrations directory}", testdock.GooseMigrateFactoryPGX),
            )

            // {Prepare isolated test data through pool.}
            // {Run code under test with pool.}
            // {Assert results through pool.}
        }
        ```
    </examples>
</testdock>
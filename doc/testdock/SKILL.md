---
name: testdock
description: Guidelines for using `github.com/n-r-w/testdock/v2` package.
---

<testdock name="github.com/n-r-w/testdock/v2 guidelines">
    <instructions>
        1. Use GetPgxPool, GetPqConn, GetMySQLConn, GetSQLConn, GetMongoDatabase, or GetMongoDatabaseV2 according to the database driver.
        2. Each Get... call creates a separate independent temporary database with a unique name.
        3. It is safe to call Get... from t.Parallel() tests; separate databases prevent database state conflicts between tests.
        4. Do not add manual cleanup for resources returned by Get...; testdock registers tb.Cleanup for database cleanup and connection closing.
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
        15. Use WithRetryTimeout and WithTotalRetryDuration only for slow startup; retry timeout must be less than total retry duration.
    </instructions>
    <examples>
        ```go
        import (
            "testing"
            "github.com/jackc/pgx/v5/pgxpool"
            "github.com/n-r-w/testdock/v2"
        )

        func newTestPool(t *testing.T) *pgxpool.Pool {
            t.Helper()
            pool, _ := testdock.GetPgxPool(t, testdock.DefaultPostgresDSN)
            return pool
        }
        ```
    </examples>
</testdock>
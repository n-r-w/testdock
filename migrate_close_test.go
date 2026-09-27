package testdock

import (
	"errors"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	databaseStub "github.com/golang-migrate/migrate/v4/database/stub"
	"github.com/golang-migrate/migrate/v4/source"
	sourceStub "github.com/golang-migrate/migrate/v4/source/stub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGolangMigrateUpClosesResources verifies successful migrations synchronously close both drivers.
func TestGolangMigrateUpClosesResources(t *testing.T) {
	t.Parallel()

	for _, targeted := range []bool{false, true} {
		t.Run(map[bool]string{false: "up", true: "up-to"}[targeted], func(t *testing.T) {
			t.Parallel()

			sourceErr := errors.New("source close failed")
			databaseErr := errors.New("database close failed")

			sourceDriver, err := sourceStub.WithInstance(nil, &sourceStub.Config{})
			require.NoError(t, err)
			appended := sourceDriver.(*sourceStub.Stub).Migrations.Append(&source.Migration{
				Version:    1,
				Identifier: "migration body",
				Direction:  source.Up,
				Raw:        "",
			})
			require.True(t, appended)
			databaseDriver, err := databaseStub.WithInstance(nil, &databaseStub.Config{})
			require.NoError(t, err)

			src := &failingMigrationSource{Driver: sourceDriver, closeErr: sourceErr, closed: false}
			db := &failingMigrationDatabase{
				Driver: databaseDriver, versionErr: nil, closeErr: databaseErr, closed: false,
			}
			engine, err := migrate.NewWithInstance("fixture", src, "fixture", db)
			require.NoError(t, err)
			migrator := &golangMigrateMigrator{m: engine}

			if targeted {
				err = migrator.UpTo(t.Context(), 1)
			} else {
				err = migrator.Up(t.Context())
			}

			require.ErrorIs(t, err, sourceErr)
			require.ErrorIs(t, err, databaseErr)
			assert.True(t, src.closed)
			assert.True(t, db.closed)
			assert.Equal(t, []string{"migration body"}, databaseDriver.(*databaseStub.Stub).MigrationSequence)
		})
	}
}

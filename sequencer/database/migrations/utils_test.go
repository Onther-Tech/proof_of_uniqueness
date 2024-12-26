package migrations_test

import (
	"fmt"
	"testing"
	"tokamak-sybil-resistance/log"

	dbUtils "tokamak-sybil-resistance/database"

	"github.com/gobuffalo/packr/v2"
	"github.com/jmoiron/sqlx"
	migrate "github.com/rubenv/sql-migrate"
	"github.com/stretchr/testify/require"
)

/*
	Considerations tricks and tips for migration file testing:

	- Functionality of the DB is tested by the rest of the packages, migration tests only have to check persistence across migrations (both UP and DOWN)
	- It's recommended to use real data (from testnet/mainnet), but modifying NULL fields to check that those are migrated properly
	- It's recommended to use some SQL tool (such as DBeaver) that generates insert queries from existing rows
	- Any new migration file could be tested using the existing `migrationTester` interface. Check `0002_test.go` for an example
*/

func init() {
	log.Init("debug", []string{"stdout"})
}

type migrationTester interface {
	// InsertData used to insert data in the affected tables of the migration that is being tested
	// data will be inserted with the schema as it was previous the migration that is being tested
	InsertData(*sqlx.DB) error
	// RunAssertsAfterMigrationUp this function will be called after running the migration is being tested
	// and should assert that the data inserted in the function InsertData is persisted properly
	RunAssertsAfterMigrationUp(*testing.T, *sqlx.DB)
	// RunAssertsAfterMigrationDown this function will be called after reverting the migration that is being tested
	// and should assert that the data inserted in the function InsertData is persisted properly
	RunAssertsAfterMigrationDown(*testing.T, *sqlx.DB)
}

func runMigrationTest(t *testing.T, migrationNumber int, miter migrationTester) {
	// Initialize an empty DB
	db, err := initCleanSQLDB()
	require.NoError(t, err)
	require.NoError(t, dbUtils.MigrationsDown(db.DB, 0))
	// Run migrations until migration to test
	require.NoError(t, runMigrationsUp(db, migrationNumber-1))
	// Insert data into table(s) affected by migration
	require.NoError(t, miter.InsertData(db))
	// Run migration that is being tested
	require.NoError(t, runMigrationsUp(db, 1))
	// Check that data is persisted properly after migration up
	miter.RunAssertsAfterMigrationUp(t, db)
	// Revert migration to test
	require.NoError(t, dbUtils.MigrationsDown(db.DB, 1))
	// Check that data is persisted properly after migration down
	miter.RunAssertsAfterMigrationDown(t, db)
}

func initCleanSQLDB() (*sqlx.DB, error) {
	return dbUtils.ConnectSQLDB()
}

func runMigrationsUp(db *sqlx.DB, n int) error {
	migrations := &migrate.PackrMigrationSource{
		Box: packr.New("hermez-db-migrations", "./migrations"),
	}
	nMigrations, err := migrate.ExecMax(db.DB, "postgres", migrations, migrate.Up, n)
	if err != nil {
		return err
	}
	if nMigrations != n {
		return fmt.Errorf("Unexpected amount of migrations: expected: %d, actual: %d", n, nMigrations)
	}
	return nil
}

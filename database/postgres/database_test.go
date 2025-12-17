//go:build !windows

package postgres

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	_ "github.com/lib/pq"
	"github.com/sqldef/sqldef/v3/database"
	"github.com/sqldef/sqldef/v3/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnixSocketConnection(t *testing.T) {
	// PostgreSQL expects socket files named .s.PGSQL.<port> in the directory
	sock := testutil.StartDummyUnixSocket(t, "postgres-socket-test", ".s.PGSQL.5432")
	defer sock.Close()

	config := database.Config{
		DbName:   "testdb",
		User:     "testuser",
		Password: "testpass",
		Socket:   sock.Dir,
		Port:     5432,
	}

	db, err := NewDatabase(config)
	if err != nil {
		t.Fatalf("NewDatabase failed: %v", err)
	}
	defer db.Close()

	err = db.DB().Ping()
	if err == nil {
		t.Fatal("expected connection to fail with protocol error")
	}

	// "connection refused" means socket path was not used (fell back to TCP).
	// Any other error (e.g., protocol error) indicates the socket was used.
	if strings.Contains(err.Error(), "connection refused") {
		t.Errorf("expected socket to be used, got: %v", err)
	}
}

// TestExtensionOIDCollisionByInjectedDependency verifies that ExportDDLs correctly
// handles OID collisions between user objects and extension objects.
//
// PostgreSQL's pg_depend stores object dependencies using (classid, objid) pairs.
// The classid identifies which catalog the object belongs to (e.g., pg_class for tables,
// pg_proc for functions). Without checking classid, a user table could be incorrectly
// filtered if its OID matches an extension function's OID in pg_depend.
//
// This test injects a fake pg_depend entry with the table's OID but a different classid
// (pg_proc) to simulate such a collision, then verifies the table is still exported.
func TestExtensionOIDCollisionByInjectedDependency(t *testing.T) {
	db := setupTestDatabase(t)
	defer db.Close()

	// Create a user table
	_, err := db.DB().Exec("CREATE TABLE collision_victim (id bigint PRIMARY KEY, name text)")
	require.NoError(t, err)

	// Get the table's OID
	var tableOID int64
	err = db.DB().QueryRow(`
		SELECT c.oid FROM pg_class c
		JOIN pg_namespace n ON c.relnamespace = n.oid
		WHERE c.relname = 'collision_victim' AND n.nspname = 'public'
	`).Scan(&tableOID)
	require.NoError(t, err)

	// Get pg_proc's classid (for functions)
	var pgProcClassID int64
	err = db.DB().QueryRow(`SELECT oid FROM pg_class WHERE relname = 'pg_proc'`).Scan(&pgProcClassID)
	require.NoError(t, err)

	// Get pg_extension's OID (we need a real extension for the refclassid/refobjid)
	_, err = db.DB().Exec("CREATE EXTENSION IF NOT EXISTS pgcrypto")
	require.NoError(t, err)

	var extOID int64
	err = db.DB().QueryRow(`SELECT oid FROM pg_extension WHERE extname = 'pgcrypto'`).Scan(&extOID)
	require.NoError(t, err)

	var pgExtensionClassID int64
	err = db.DB().QueryRow(`SELECT oid FROM pg_class WHERE relname = 'pg_extension'`).Scan(&pgExtensionClassID)
	require.NoError(t, err)

	t.Logf("Table OID: %d", tableOID)
	t.Logf("pg_proc classid: %d", pgProcClassID)
	t.Logf("pgcrypto extension OID: %d", extOID)

	// Inject a fake pg_depend entry that simulates an OID collision:
	// - objid = table's OID (collision!)
	// - classid = pg_proc (as if it were a function, not a table)
	// - deptype = 'e' (extension dependency)
	// This simulates the scenario where a table's OID matches an extension function's OID
	_, err = db.DB().Exec(`
		INSERT INTO pg_depend (classid, objid, objsubid, refclassid, refobjid, refobjsubid, deptype)
		VALUES ($1, $2, 0, $3, $4, 0, 'e')
	`, pgProcClassID, tableOID, pgExtensionClassID, extOID)
	require.NoError(t, err, "Failed to inject fake pg_depend entry - need superuser privileges")

	t.Log("Injected fake pg_depend entry simulating OID collision")

	// Verify the fake entry exists
	var fakeEntryExists bool
	err = db.DB().QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM pg_depend
			WHERE objid = $1 AND classid = $2 AND deptype = 'e'
		)
	`, tableOID, pgProcClassID).Scan(&fakeEntryExists)
	require.NoError(t, err)
	require.True(t, fakeEntryExists, "Fake pg_depend entry should exist")

	// Verify that a query without classid check would incorrectly see our table
	// as extension-dependent, while the actual ExportDDLs (with classid check) works correctly
	var queryWithoutClassidExcludes bool
	err = db.DB().QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM pg_depend d
			WHERE d.objid = $1 AND d.deptype = 'e'
		)
	`, tableOID).Scan(&queryWithoutClassidExcludes)
	require.NoError(t, err)

	t.Logf("Query without classid check would exclude table: %v", queryWithoutClassidExcludes)
	assert.True(t, queryWithoutClassidExcludes, "Query without classid should see fake extension dependency")

	// Verify ExportDDLs correctly includes the table despite the fake pg_depend entry
	db.SetGeneratorConfig(database.GeneratorConfig{
		LegacyIgnoreQuotes: true,
	})
	exported, err := db.ExportDDLs()
	require.NoError(t, err)

	t.Logf("Exported DDL contains collision_victim: %v", strings.Contains(exported, "collision_victim"))

	assert.Contains(t, exported, "collision_victim",
		"Table should be exported despite fake pg_depend entry with different classid")

	// Clean up the fake dependency
	_, _ = db.DB().Exec(`
		DELETE FROM pg_depend
		WHERE objid = $1 AND classid = $2 AND deptype = 'e'
	`, tableOID, pgProcClassID)
}

// TestExtensionOIDCollisionForViews verifies that views are correctly exported
// even when a fake extension dependency with a different classid exists.
func TestExtensionOIDCollisionForViews(t *testing.T) {
	db := setupTestDatabase(t)
	defer db.Close()

	// Create a user view
	_, err := db.DB().Exec("CREATE TABLE view_base (id int)")
	require.NoError(t, err)
	_, err = db.DB().Exec("CREATE VIEW collision_victim_view AS SELECT * FROM view_base")
	require.NoError(t, err)

	// Get the view's OID
	var viewOID int64
	err = db.DB().QueryRow(`
		SELECT c.oid FROM pg_class c
		JOIN pg_namespace n ON c.relnamespace = n.oid
		WHERE c.relname = 'collision_victim_view' AND n.nspname = 'public'
	`).Scan(&viewOID)
	require.NoError(t, err)

	// Get pg_proc's classid and extension info
	var pgProcClassID int64
	err = db.DB().QueryRow(`SELECT oid FROM pg_class WHERE relname = 'pg_proc'`).Scan(&pgProcClassID)
	require.NoError(t, err)

	_, err = db.DB().Exec("CREATE EXTENSION IF NOT EXISTS pgcrypto")
	require.NoError(t, err)

	var extOID int64
	err = db.DB().QueryRow(`SELECT oid FROM pg_extension WHERE extname = 'pgcrypto'`).Scan(&extOID)
	require.NoError(t, err)

	var pgExtensionClassID int64
	err = db.DB().QueryRow(`SELECT oid FROM pg_class WHERE relname = 'pg_extension'`).Scan(&pgExtensionClassID)
	require.NoError(t, err)

	// Inject fake dependency
	_, err = db.DB().Exec(`
		INSERT INTO pg_depend (classid, objid, objsubid, refclassid, refobjid, refobjsubid, deptype)
		VALUES ($1, $2, 0, $3, $4, 0, 'e')
	`, pgProcClassID, viewOID, pgExtensionClassID, extOID)
	require.NoError(t, err)

	// Export and verify view is included
	db.SetGeneratorConfig(database.GeneratorConfig{
		LegacyIgnoreQuotes: true,
	})
	exported, err := db.ExportDDLs()
	require.NoError(t, err)

	assert.Contains(t, exported, "collision_victim_view",
		"View should be exported despite fake pg_depend entry with different classid")

	// Cleanup
	_, _ = db.DB().Exec(`DELETE FROM pg_depend WHERE objid = $1 AND classid = $2 AND deptype = 'e'`,
		viewOID, pgProcClassID)
}

// TestExtensionOIDCollisionForFunctions verifies that functions are correctly exported
// even when a fake extension dependency with a different classid exists.
func TestExtensionOIDCollisionForFunctions(t *testing.T) {
	db := setupTestDatabase(t)
	defer db.Close()

	// Create a user function
	_, err := db.DB().Exec(`
		CREATE FUNCTION collision_victim_func() RETURNS int AS $$
		BEGIN RETURN 1; END;
		$$ LANGUAGE plpgsql
	`)
	require.NoError(t, err)

	// Get the function's OID
	var funcOID int64
	err = db.DB().QueryRow(`
		SELECT p.oid FROM pg_proc p
		JOIN pg_namespace n ON p.pronamespace = n.oid
		WHERE p.proname = 'collision_victim_func' AND n.nspname = 'public'
	`).Scan(&funcOID)
	require.NoError(t, err)

	// Get pg_type's classid (different from pg_proc)
	var pgTypeClassID int64
	err = db.DB().QueryRow(`SELECT oid FROM pg_class WHERE relname = 'pg_type'`).Scan(&pgTypeClassID)
	require.NoError(t, err)

	_, err = db.DB().Exec("CREATE EXTENSION IF NOT EXISTS pgcrypto")
	require.NoError(t, err)

	var extOID int64
	err = db.DB().QueryRow(`SELECT oid FROM pg_extension WHERE extname = 'pgcrypto'`).Scan(&extOID)
	require.NoError(t, err)

	var pgExtensionClassID int64
	err = db.DB().QueryRow(`SELECT oid FROM pg_class WHERE relname = 'pg_extension'`).Scan(&pgExtensionClassID)
	require.NoError(t, err)

	// Inject fake dependency with pg_type classid (not pg_proc)
	_, err = db.DB().Exec(`
		INSERT INTO pg_depend (classid, objid, objsubid, refclassid, refobjid, refobjsubid, deptype)
		VALUES ($1, $2, 0, $3, $4, 0, 'e')
	`, pgTypeClassID, funcOID, pgExtensionClassID, extOID)
	require.NoError(t, err)

	// Export and verify function is included
	db.SetGeneratorConfig(database.GeneratorConfig{
		LegacyIgnoreQuotes: true,
	})
	exported, err := db.ExportDDLs()
	require.NoError(t, err)

	assert.Contains(t, exported, "collision_victim_func",
		"Function should be exported despite fake pg_depend entry with different classid")

	// Cleanup
	_, _ = db.DB().Exec(`DELETE FROM pg_depend WHERE objid = $1 AND classid = $2 AND deptype = 'e'`,
		funcOID, pgTypeClassID)
}

// Helper functions

func setupTestDatabase(t *testing.T) *PostgresDatabase {
	t.Helper()

	host := "127.0.0.1"
	if h := os.Getenv("PGHOST"); h != "" {
		host = h
	}

	port := 5432
	if p := os.Getenv("PGPORT"); p != "" {
		if pInt, err := strconv.Atoi(p); err == nil {
			port = pInt
		}
	}

	user := "postgres"
	if u := os.Getenv("PGUSER"); u != "" {
		user = u
	}

	password := os.Getenv("PGPASSWORD")
	sslMode := "disable"
	if s := os.Getenv("PGSSLMODE"); s != "" {
		sslMode = s
	}

	dbName := "psqldef_database_test"

	// Connect to postgres database to recreate test database
	adminDSN := fmt.Sprintf("postgres://%s:%s@%s:%d/postgres?sslmode=%s",
		user, password, host, port, sslMode)
	adminDB, err := sql.Open("postgres", adminDSN)
	require.NoError(t, err)

	_, _ = adminDB.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
	_, err = adminDB.Exec(fmt.Sprintf("CREATE DATABASE %s", dbName))
	require.NoError(t, err)
	adminDB.Close()

	// Connect to test database
	db, err := NewDatabase(database.Config{
		User:     user,
		Password: password,
		Host:     host,
		Port:     port,
		DbName:   dbName,
		SslMode:  sslMode,
	})
	require.NoError(t, err)

	return db.(*PostgresDatabase)
}

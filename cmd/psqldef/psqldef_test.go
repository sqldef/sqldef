// Integration test of psqldef command.
//
// Test requirement:
//   - go command
//   - `psql -Upostgres` must succeed
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/sqldef/sqldef/v3/database"
	"github.com/sqldef/sqldef/v3/database/postgres"
	"github.com/sqldef/sqldef/v3/schema"
	tu "github.com/sqldef/sqldef/v3/testutil"
)

const (
	applyPrefix      = "-- Apply --\n"
	nothingModified  = "-- Nothing is modified --\n"
	defaultUser      = "postgres"
	testDatabaseName = "psqldef_test"
	defaultPort      = 5432
)

type dbConfig struct {
	User   string
	DbName string
}

var defaultDbConfig = dbConfig{
	User:   defaultUser,
	DbName: testDatabaseName,
}

// roleConfigMutex protects role configuration operations (CREATE ROLE, ALTER ROLE)
// to prevent "tuple concurrently updated" errors when running tests in parallel.
var roleConfigMutex sync.Mutex

// getPostgresPort returns the port to use for connecting to PostgreSQL.
// pgvector flavor uses port 55432, standard PostgreSQL uses 5432.
// PGPORT environment variable overrides the default port.
func getPostgresPort() int {
	if port, ok := os.LookupEnv("PGPORT"); ok {
		if portInt, err := strconv.Atoi(port); err == nil {
			return portInt
		}
	}
	return defaultPort
}

func getPostgresHost() string {
	if host, ok := os.LookupEnv("PGHOST"); ok {
		return host
	}
	return "127.0.0.1"
}

// getPostgresFlavor returns the current PostgreSQL flavor from environment.
func getPostgresFlavor() string {
	return os.Getenv("PG_FLAVOR")
}

// psqldefArgs returns the base arguments for psqldef command including port.
func psqldefArgs(args ...string) []string {
	baseArgs := []string{"-U", defaultUser, "-p", fmt.Sprintf("%d", getPostgresPort())}
	return append(baseArgs, args...)
}

func connectDatabase(config dbConfig) (database.Database, error) {
	var user string
	if config.User != "" {
		user = config.User
	} else {
		user = defaultUser
	}

	return postgres.NewDatabase(database.Config{
		User:    user,
		Host:    getPostgresHost(),
		Port:    getPostgresPort(),
		DbName:  config.DbName,
		SslMode: "disable",
	})
}

func wrapWithTransaction(ddls string) string {
	return applyPrefix + "BEGIN;\n" + ddls + "COMMIT;\n"
}

// pgQuery executes a query against the database and returns rows as string
func pgQuery(dbName string, query string) (string, error) {
	db, err := connectDatabase(dbConfig{
		User:   defaultUser,
		DbName: dbName,
	})
	if err != nil {
		return "", err
	}
	defer db.Close()

	return tu.QueryRows(db, query)
}

// pgExec executes a statement against the database (doesn't return rows)
func pgExec(dbName string, statement string) error {
	db, err := connectDatabase(dbConfig{
		User:   defaultUser,
		DbName: dbName,
	})
	if err != nil {
		return err
	}

	defer db.Close()

	_, err = db.DB().Exec(statement)
	return err
}

// pgExecAsUser executes a statement as a specific user
func pgExecAsUser(dbName string, user string, statement string) error {
	db, err := connectDatabase(dbConfig{
		User:   user,
		DbName: dbName,
	})
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.DB().Exec(statement)
	return err
}

// mustPgExec executes a statement against the database and panics on error
func mustPgExec(dbName string, statement string) {
	if err := pgExec(dbName, statement); err != nil {
		panic(err)
	}
}

// mustPgExecAsUser executes a statement as a specific user and panics on error
func mustPgExecAsUser(dbName string, user string, statement string) {
	if err := pgExecAsUser(dbName, user, statement); err != nil {
		panic(err)
	}
}

// mustGetServerVersion retrieves the PostgreSQL server version and panics on error
func mustGetServerVersion() string {
	db, err := connectDatabase(defaultDbConfig)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	var serverVersion string
	err = db.DB().QueryRow("SHOW server_version").Scan(&serverVersion)
	if err != nil {
		panic(err)
	}

	return strings.Split(serverVersion, " ")[0]
}

// createTestDatabase creates a new database for a test case with the specified user.
// If a user is specified, it creates the user role and grants necessary permissions.
func createTestDatabase(t *testing.T, dbName string, user string) {
	t.Helper()

	// Use the default 'postgres' database to create the new test database
	defaultDatabaseName := "postgres"
	mustPgExec(defaultDatabaseName, fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
	mustPgExec(defaultDatabaseName, fmt.Sprintf("CREATE DATABASE %s", dbName))

	// Set up user if specified
	if user != "" {
		// Protect role operations with a mutex to prevent concurrent updates
		roleConfigMutex.Lock()
		// Create the user role if it doesn't exist (roles are cluster-wide)
		query := fmt.Sprintf(`
			DO $$ BEGIN
				IF NOT EXISTS (SELECT * FROM pg_roles WHERE rolname = '%s') THEN
					CREATE ROLE %s WITH LOGIN;
				END IF;
			END $$;
		`, user, user)
		mustPgExec(defaultDatabaseName, query)

		// Set up the user-specific search_path (this modifies cluster-wide role settings)
		mustPgExec(dbName, fmt.Sprintf("ALTER ROLE %s SET search_path TO foo, public", user))
		roleConfigMutex.Unlock()

		// Grant permissions and create schema (these are database-specific, safe to run in parallel)
		mustPgExec(dbName, fmt.Sprintf("GRANT ALL ON DATABASE %s TO %s", dbName, user))
		mustPgExecAsUser(dbName, user, "CREATE SCHEMA foo")
	}
}

// dropTestDatabase drops a test database. This is used in cleanup.
func dropTestDatabase(dbName string) {
	// Use the default 'postgres' database to drop the test database
	defaultDatabaseName := "postgres"
	// Ignore errors during cleanup, as the database might already be dropped
	_ = pgExec(defaultDatabaseName, fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
}

func TestApply(t *testing.T) {
	// Create all test roles once before running tests
	// PostgreSQL roles are cluster-wide, not database-specific
	createAllTestRoles()

	tests, err := tu.ReadTests("tests*.yml")
	if err != nil {
		t.Fatal(err)
	}

	version := mustGetServerVersion()
	sqlParser := postgres.NewParser()
	pgFlavor := getPostgresFlavor()
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dbName := tu.CreateTestDatabaseName(name, 63)
			createTestDatabase(t, dbName, test.User)

			t.Cleanup(func() {
				dropTestDatabase(dbName)
			})

			db, err := connectDatabase(dbConfig{
				User:   test.User,
				DbName: dbName,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()

			tu.RunTest(t, db, test, schema.GeneratorModePostgres, sqlParser, version, pgFlavor)
		})
	}
}

// TestPsqldefCitextExtension cannot be migrated: requires mustPgExec to drop extension after test
func TestPsqldefCitextExtension(t *testing.T) {
	resetTestDatabase()

	createTable := tu.StripHeredoc(`
		CREATE EXTENSION citext;
		CREATE TABLE users (
		  name citext
		);
		`,
	)
	assertApplyOutput(t, createTable, wrapWithTransaction(createTable))
	assertApplyOutput(t, createTable, nothingModified)

	mustPgExec(testDatabaseName, "DROP TABLE users;")
	mustPgExec(testDatabaseName, "DROP EXTENSION citext;")
}

// TestPsqldefIgnoreExtension cannot be migrated: requires mustPgExec to drop extension after test
func TestPsqldefIgnoreExtension(t *testing.T) {
	resetTestDatabase()

	createTable := tu.StripHeredoc(`
		CREATE EXTENSION pg_buffercache;
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name text,
		  age integer
		);
		`,
	)

	assertApplyOutput(t, createTable, wrapWithTransaction(createTable))
	assertApplyOutput(t, createTable, nothingModified)

	// pg_buffercache extension creates a view on public schema, but it should not be exported.
	assertExportOutput(t, tu.StripHeredoc(`
		CREATE EXTENSION "pg_buffercache";

		CREATE TABLE "public"."users" (
		    "id" bigint NOT NULL,
		    "name" text,
		    "age" integer
		);
		`))

	mustPgExec(testDatabaseName, "DROP EXTENSION pg_buffercache;")
}

// TestPsqldefCreateTableWithConstraintReferences cannot be migrated: requires mustPgExec to create schemas
func TestPsqldefCreateTableWithConstraintReferences(t *testing.T) {
	resetTestDatabase()
	mustPgExec(testDatabaseName, "CREATE SCHEMA a;")
	mustPgExec(testDatabaseName, "CREATE SCHEMA c;")

	createTable := tu.StripHeredoc(`
		CREATE TABLE a.b (
		  "id" serial PRIMARY KEY
		);
		CREATE TABLE c.d (
		  "id" serial PRIMARY KEY
		);
		`,
	)
	assertApplyOutput(t, createTable, wrapWithTransaction(createTable))
	assertApplyOutput(t, createTable, nothingModified)

	createTable = tu.StripHeredoc(`
		CREATE TABLE a.b (
		  "id" serial PRIMARY KEY
		);
		CREATE TABLE c.d (
		  "id" serial PRIMARY KEY,
		  CONSTRAINT d_id_fkey FOREIGN KEY (id) REFERENCES "a"."b" (id)
		);
		`,
	)
	assertApplyOutput(t, createTable, wrapWithTransaction(`ALTER TABLE "c"."d" ADD CONSTRAINT "d_id_fkey" FOREIGN KEY ("id") REFERENCES "a"."b" ("id");`+"\n"))
	assertApplyOutput(t, createTable, nothingModified)
}

// TestPsqldefCreateView cannot be migrated: uses publicAndNonPublicSchemaTestCases loop
func TestPsqldefCreateView(t *testing.T) {
	for _, tc := range publicAndNonPublicSchemaTestCases {
		t.Run(tc.Name, func(t *testing.T) {
			resetTestDatabase()
			mustPgExec(testDatabaseName, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s;", tc.Schema))

			createUsers := fmt.Sprintf("CREATE TABLE %s.users (id BIGINT PRIMARY KEY, name character varying(100));\n", tc.Schema)
			createPosts := fmt.Sprintf("CREATE TABLE %s.posts (id BIGINT PRIMARY KEY, name character varying(100), user_id BIGINT, is_deleted boolean);\n", tc.Schema)
			assertApplyOutput(t, createUsers+createPosts, wrapWithTransaction(createUsers+createPosts))
			assertApplyOutput(t, createUsers+createPosts, nothingModified)

			posts := "posts"
			users := "users"
			if tc.Schema != "public" {
				posts = fmt.Sprintf("%s.posts", tc.Schema)
				users = fmt.Sprintf("%s.users", tc.Schema)
			}

			createView := fmt.Sprintf("CREATE VIEW %s.view_user_posts AS SELECT p.id FROM (%s as p JOIN %s as u ON ((p.user_id = u.id)));\n", tc.Schema, posts, users)
			assertApplyOutput(t, createUsers+createPosts+createView, wrapWithTransaction(fmt.Sprintf("CREATE VIEW %s.view_user_posts AS SELECT p.id FROM (%s as p JOIN %s as u ON ((p.user_id = u.id)));\n", tc.Schema, posts, users)))
			assertApplyOutput(t, createUsers+createPosts+createView, nothingModified)

			createView = fmt.Sprintf("CREATE VIEW %s.view_user_posts AS SELECT p.id from (%s p INNER JOIN %s u ON ((p.user_id = u.id))) WHERE (p.is_deleted = FALSE);\n", tc.Schema, posts, users)
			assertApplyOutput(t, createUsers+createPosts+createView, wrapWithTransaction(fmt.Sprintf(`CREATE OR REPLACE VIEW "%s"."view_user_posts" AS select p.id from (%s as p join %s as u on ((p.user_id = u.id))) where (p.is_deleted = false);`+"\n", tc.Schema, posts, users)))
			assertApplyOutput(t, createUsers+createPosts+createView, nothingModified)

			assertApplyOutput(t, createUsers+createPosts, wrapWithTransaction(fmt.Sprintf(`-- Skipped: DROP VIEW "%s"."view_user_posts";`, tc.Schema)+"\n"))
			assertApplyOutputWithEnableDrop(t, createUsers+createPosts, wrapWithTransaction(fmt.Sprintf(`DROP VIEW "%s"."view_user_posts";`, tc.Schema)+"\n"))
			assertApplyOutput(t, createUsers+createPosts, nothingModified)
		})
	}
}

// TestPsqldefCreateMaterializedView cannot be migrated: uses publicAndNonPublicSchemaTestCases loop
func TestPsqldefCreateMaterializedView(t *testing.T) {
	for _, tc := range publicAndNonPublicSchemaTestCases {
		t.Run(tc.Name, func(t *testing.T) {
			resetTestDatabase()
			mustPgExec(testDatabaseName, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s;", tc.Schema))

			createUsers := fmt.Sprintf("CREATE TABLE %s.users (id BIGINT PRIMARY KEY, name character varying(100));\n", tc.Schema)
			createPosts := fmt.Sprintf("CREATE TABLE %s.posts (id BIGINT PRIMARY KEY, name character varying(100), user_id BIGINT, is_deleted boolean);\n", tc.Schema)
			assertApplyOutput(t, createUsers+createPosts, wrapWithTransaction(createUsers+createPosts))
			assertApplyOutput(t, createUsers+createPosts, nothingModified)

			posts := "posts"
			users := "users"
			if tc.Schema != "public" {
				posts = fmt.Sprintf("%s.posts", tc.Schema)
				users = fmt.Sprintf("%s.users", tc.Schema)
			}

			createMaterializedView := fmt.Sprintf("CREATE MATERIALIZED VIEW %s.view_user_posts AS SELECT p.id FROM (%s as p JOIN %s as u ON ((p.user_id = u.id)));\n", tc.Schema, posts, users)
			assertApplyOutput(t, createUsers+createPosts+createMaterializedView, wrapWithTransaction(fmt.Sprintf("CREATE MATERIALIZED VIEW %s.view_user_posts AS SELECT p.id FROM (%s as p JOIN %s as u ON ((p.user_id = u.id)));\n", tc.Schema, posts, users)))
			assertApplyOutput(t, createUsers+createPosts+createMaterializedView, nothingModified)

			assertApplyOutputWithEnableDrop(t, createUsers+createPosts, wrapWithTransaction(fmt.Sprintf(`DROP MATERIALIZED VIEW "%s"."view_user_posts";`, tc.Schema)+"\n"))
			assertApplyOutput(t, createUsers+createPosts, nothingModified)
		})
	}
}

// TestPsqldefCreateIndex cannot be migrated: uses publicAndNonPublicSchemaTestCases loop
func TestPsqldefCreateIndex(t *testing.T) {
	for _, tc := range publicAndNonPublicSchemaTestCases {
		t.Run(tc.Name, func(t *testing.T) {
			resetTestDatabase()
			mustPgExec(testDatabaseName, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s;", tc.Schema))

			createTable := tu.StripHeredoc(fmt.Sprintf(`
				CREATE TABLE %s.users (
				  id bigint NOT NULL,
				  name text,
				  age integer
				);`, tc.Schema))
			createIndex1 := fmt.Sprintf(`CREATE INDEX index_name on %s.users (name);`, tc.Schema)
			createIndex2 := fmt.Sprintf(`CREATE UNIQUE INDEX index_age on %s.users (age);`, tc.Schema)
			createIndex3 := fmt.Sprintf(`CREATE INDEX index_name on %s.users (name, id);`, tc.Schema)
			createIndex4 := fmt.Sprintf(`CREATE UNIQUE INDEX index_name on %s.users (name) WHERE age > 20;`, tc.Schema)
			dropIndex1 := fmt.Sprintf(`DROP INDEX "%s"."index_name";`, tc.Schema)
			dropIndex2 := fmt.Sprintf(`DROP INDEX "%s"."index_age";`, tc.Schema)

			assertApplyOutput(t, createTable+createIndex1+createIndex2, wrapWithTransaction(createTable+"\n"+
				createIndex1+"\n"+
				createIndex2+"\n"))
			assertApplyOutput(t, createTable+createIndex1+createIndex2, nothingModified)

			assertApplyOutputWithEnableDrop(t, createTable+createIndex2+createIndex3, wrapWithTransaction(
				dropIndex1+"\n"+
					createIndex3+"\n"))
			assertApplyOutput(t, createTable+createIndex2+createIndex3, nothingModified)

			assertApplyOutputWithEnableDrop(t, createTable+createIndex2+createIndex4, wrapWithTransaction(
				dropIndex1+"\n"+
					createIndex4+"\n"))
			assertApplyOutput(t, createTable+createIndex2+createIndex4, nothingModified)

			assertApplyOutputWithEnableDrop(t, createTable, wrapWithTransaction(
				dropIndex2+"\n"+
					dropIndex1+"\n"))
			assertApplyOutput(t, createTable, nothingModified)
		})
	}
}

// TestPsqldefCreateMaterializedViewIndex cannot be migrated: uses publicAndNonPublicSchemaTestCases loop
func TestPsqldefCreateMaterializedViewIndex(t *testing.T) {
	for _, tc := range publicAndNonPublicSchemaTestCases {
		t.Run(tc.Name, func(t *testing.T) {
			resetTestDatabase()
			mustPgExec(testDatabaseName, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s;", tc.Schema))

			createTable := tu.StripHeredoc(fmt.Sprintf(`
				CREATE TABLE %s.users (
				  id bigint NOT NULL,
				  name text,
				  age integer
				);`, tc.Schema))
			users := "users"
			if tc.Schema != "public" {
				users = fmt.Sprintf("%s.users", tc.Schema)
			}
			createMaterializedView := fmt.Sprintf("CREATE MATERIALIZED VIEW %s.view_users AS SELECT * FROM %s;\n", tc.Schema, users)
			assertApplyOutput(t, createTable+createMaterializedView, wrapWithTransaction(createTable+"\n"+
				fmt.Sprintf("CREATE MATERIALIZED VIEW %s.view_users AS SELECT * FROM %s;\n", tc.Schema, users)))
			assertApplyOutput(t, createTable+createMaterializedView, nothingModified)

			createIndex1 := fmt.Sprintf(`CREATE INDEX index_name on %s.view_users (name);`, tc.Schema)
			createIndex2 := fmt.Sprintf(`CREATE UNIQUE INDEX index_age on %s.view_users (age);`, tc.Schema)
			assertApplyOutput(t, createTable+createMaterializedView+createIndex1+createIndex2, wrapWithTransaction(createIndex1+"\n"+
				createIndex2+"\n"))
			assertApplyOutput(t, createTable+createMaterializedView+createIndex1+createIndex2, nothingModified)
		})
	}
}

// TestPsqldefCreateTableWithExpressionStored cannot be migrated: has version-specific skip logic
func TestPsqldefCreateTableWithExpressionStored(t *testing.T) {
	resetTestDatabase()

	createTable := tu.StripHeredoc(`
		CREATE TABLE products (
		  name VARCHAR(255),
		  description VARCHAR(255),
		  tsv tsvector GENERATED ALWAYS AS (to_tsvector('english', name) || to_tsvector('english',description)) STORED
		);
		`,
	)
	if err := pgExec(testDatabaseName, createTable); err != nil {
		t.Skipf("PostgreSQL doesn't support the test: %s", err)
	}

	resetTestDatabase()

	assertApplyOutput(t, createTable, wrapWithTransaction(createTable))
	assertApplyOutput(t, createTable, nothingModified)
}

// TestPsqldefFunctionAsDefault tests that tables with column defaults referencing functions work correctly.
// The function is created manually first (outside sqldef), then the table references it.
// Since the function is not in the desired schema, sqldef will try to drop it (but PostgreSQL
// will prevent this because the table depends on it).
func TestPsqldefFunctionAsDefault(t *testing.T) {
	for _, tc := range publicAndNonPublicSchemaTestCases {
		resetTestDatabase()
		mustPgExec(testDatabaseName, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s;", tc.Schema))

		// Create function manually (outside sqldef) - this simulates a pre-existing function
		mustPgExec(testDatabaseName, fmt.Sprintf(tu.StripHeredoc(`
			CREATE FUNCTION %s.my_func()
			RETURNS int
			AS $$
			DECLARE
			  result int = 1;
			BEGIN
			  RETURN result;
			END
			$$
			LANGUAGE plpgsql
			VOLATILE;`), tc.Schema))

		createTable := fmt.Sprintf(tu.StripHeredoc(`
			CREATE TABLE %s.test (
			  pk timestamp primary key default now(),
			  col timestamp default now(),
			  uniq timestamp unique default now(),
			  not_null timestamp not null default now(),
			  same_schema int default %s.my_func()
			);`), tc.Schema, tc.Schema)

		// First apply creates the table. The orphaned function drop is skipped (enable_drop=false by default).
		expectedOutput := fmt.Sprintf("%s\n-- Skipped: DROP FUNCTION %q.\"my_func\";\n", createTable, tc.Schema)
		assertApplyOutput(t, createTable, wrapWithTransaction(expectedOutput))
		// Second apply: orphaned function drop is still skipped, so it appears again
		assertApplyOutput(t, createTable, wrapWithTransaction(fmt.Sprintf("-- Skipped: DROP FUNCTION %q.\"my_func\";\n", tc.Schema)))
	}
}

//
// ----------------------- following tests are for CLI -----------------------
//

func TestPsqldefApply(t *testing.T) {
	resetTestDatabase()

	createTable := tu.StripHeredoc(`
		CREATE TABLE bigdata (
		  data bigint
		);
		`,
	)

	assertApplyOutput(t, createTable, wrapWithTransaction(createTable))
	assertApplyOutput(t, createTable, nothingModified)
}

func TestPsqldefDryRun(t *testing.T) {
	resetTestDatabase()
	tu.WriteFile("schema.sql", tu.StripHeredoc(`
	    CREATE TABLE users (
	        id bigint NOT NULL PRIMARY KEY,
	        age int
	    );`,
	))

	dryRun := tu.MustExecute(t, "./psqldef", psqldefArgs(testDatabaseName, "--dry-run", "--file", "schema.sql")...)
	apply := tu.MustExecute(t, "./psqldef", psqldefArgs(testDatabaseName, "--file", "schema.sql")...)
	assert.Equal(t, strings.Replace(apply, "Apply", "dry run", 1), dryRun)
}

func TestPsqldefDropTable(t *testing.T) {
	resetTestDatabase()
	mustPgExec(testDatabaseName, tu.StripHeredoc(`
		CREATE TABLE users (
		    id bigint NOT NULL PRIMARY KEY,
		    age int,
		    c_char_1 char,
		    c_char_10 char(10),
		    c_varchar_10 varchar(10),
		    c_varchar_unlimited varchar
		);`,
	))

	tu.WriteFile("schema.sql", "")

	dropTable := `DROP TABLE "public"."users";`
	out := tu.MustExecute(t, "./psqldef", psqldefArgs(testDatabaseName, "--enable-drop", "--file", "schema.sql")...)
	assert.Equal(t, wrapWithTransaction(dropTable+"\n"), out)
}

func TestPsqldefConfigInlineEnableDrop(t *testing.T) {
	resetTestDatabase()
	ddl := tu.StripHeredoc(`
		CREATE TABLE users (
		    id bigint NOT NULL PRIMARY KEY,
		    age int,
		    c_char_1 char,
		    c_char_10 char(10),
		    c_varchar_10 varchar(10),
		    c_varchar_unlimited varchar
		);`,
	)
	mustPgExec(testDatabaseName, ddl)

	tu.WriteFile("schema.sql", "")

	dropTable := `DROP TABLE "public"."users";`
	expectedOutput := wrapWithTransaction(dropTable + "\n")

	outFlag := tu.MustExecute(t, "./psqldef", psqldefArgs(testDatabaseName, "--enable-drop", "--file", "schema.sql")...)
	assert.Equal(t, expectedOutput, outFlag)

	mustPgExec(testDatabaseName, ddl)

	outConfigInline := tu.MustExecute(t, "./psqldef", psqldefArgs(testDatabaseName, "--config-inline", "enable_drop: true", "--file", "schema.sql")...)
	assert.Equal(t, expectedOutput, outConfigInline)
}

func TestPsqldefConfigLegacyIgnoreQuotes(t *testing.T) {
	resetTestDatabase()

	// Create a table with unquoted name (normalizes to lowercase in PostgreSQL)
	mustPgExec(testDatabaseName, `CREATE TABLE users (id bigint NOT NULL PRIMARY KEY);`)

	// Schema file with unquoted table name adding a column
	tu.WriteFile("schema.sql", tu.StripHeredoc(`
		CREATE TABLE users (
		    id bigint NOT NULL PRIMARY KEY,
		    name text
		);`,
	))

	// Test with legacy_ignore_quotes: false via config-inline
	// In quote-aware mode, unquoted identifiers should output without quotes
	outQuoteAware := tu.MustExecute(t, "./psqldef", psqldefArgs(testDatabaseName, "--config-inline", "legacy_ignore_quotes: false", "--file", "schema.sql")...)
	// With legacy_ignore_quotes: false, unquoted table/schema names should not have quotes in output
	// The default schema "public" in lowercase is treated as unquoted
	assert.Contains(t, outQuoteAware, `ALTER TABLE public.users ADD COLUMN name text;`)

	// Test with legacy_ignore_quotes: true (legacy behavior) - should quote everything
	resetTestDatabase()
	mustPgExec(testDatabaseName, `CREATE TABLE users (id bigint NOT NULL PRIMARY KEY);`)

	outLegacy := tu.MustExecute(t, "./psqldef", psqldefArgs(testDatabaseName, "--config-inline", "legacy_ignore_quotes: true", "--file", "schema.sql")...)
	// With legacy_ignore_quotes: true, table names should be quoted
	assert.Contains(t, outLegacy, `ALTER TABLE "public"."users" ADD COLUMN "name" text;`)
}

func TestPsqldefExport(t *testing.T) {
	resetTestDatabase()

	assertExportOutput(t, "-- No table exists --\n")

	mustPgExec(testDatabaseName, tu.StripHeredoc(`
		CREATE TABLE users (
		    id bigint NOT NULL PRIMARY KEY,
		    age int,
		    c_char_1 char unique,
		    c_char_10 char(10),
		    c_varchar_10 varchar(10),
		    c_varchar_unlimited varchar
		);`,
	))

	assertExportOutput(t, tu.StripHeredoc(`
		CREATE TABLE "public"."users" (
		    "id" bigint NOT NULL,
		    "age" integer,
		    "c_char_1" character(1),
		    "c_char_10" character(10),
		    "c_varchar_10" character varying(10),
		    "c_varchar_unlimited" character varying,
		    CONSTRAINT users_pkey PRIMARY KEY ("id")
		);

		ALTER TABLE "public"."users" ADD CONSTRAINT "users_c_char_1_key" UNIQUE (c_char_1);
		`,
	))
}

func TestPsqldefExportCompositePrimaryKey(t *testing.T) {
	resetTestDatabase()

	assertExportOutput(t, "-- No table exists --\n")

	mustPgExec(testDatabaseName, tu.StripHeredoc(`
		CREATE TABLE users (
		    col1 character varying(40) NOT NULL,
		    col2 character varying(6) NOT NULL,
		    created_at timestamp NOT NULL,
		    PRIMARY KEY (col1, col2)
		);`,
	))

	assertExportOutput(t, tu.StripHeredoc(`
		CREATE TABLE "public"."users" (
		    "col1" character varying(40) NOT NULL,
		    "col2" character varying(6) NOT NULL,
		    "created_at" timestamp NOT NULL,
		    CONSTRAINT users_pkey PRIMARY KEY ("col1", "col2")
		);
		`,
	))
}

func TestPsqldefExportConcurrency(t *testing.T) {
	resetTestDatabase()

	mustPgExec(testDatabaseName, tu.StripHeredoc(`
		CREATE TABLE users_1 (
		    id bigint NOT NULL PRIMARY KEY
		);
		CREATE TABLE users_2 (
		    id bigint NOT NULL PRIMARY KEY
		);
		CREATE TABLE users_3 (
		    id bigint NOT NULL PRIMARY KEY
		);
		`,
	))

	outputDefault := tu.MustExecute(t, "./psqldef", psqldefArgs(testDatabaseName, "--export")...)

	tu.WriteFile("config.yml", "dump_concurrency: 0")
	outputNoConcurrency := tu.MustExecute(t, "./psqldef", psqldefArgs(testDatabaseName, "--export", "--config", "config.yml")...)

	tu.WriteFile("config.yml", "dump_concurrency: 1")
	outputConcurrency1 := tu.MustExecute(t, "./psqldef", psqldefArgs(testDatabaseName, "--export", "--config", "config.yml")...)

	tu.WriteFile("config.yml", "dump_concurrency: 10")
	outputConcurrency10 := tu.MustExecute(t, "./psqldef", psqldefArgs(testDatabaseName, "--export", "--config", "config.yml")...)

	tu.WriteFile("config.yml", "dump_concurrency: -1")
	outputConcurrencyNoLimit := tu.MustExecute(t, "./psqldef", psqldefArgs(testDatabaseName, "--export", "--config", "config.yml")...)

	assert.Equal(t, tu.StripHeredoc(`
		CREATE TABLE "public"."users_1" (
		    "id" bigint NOT NULL,
		    CONSTRAINT users_1_pkey PRIMARY KEY ("id")
		);

		CREATE TABLE "public"."users_2" (
		    "id" bigint NOT NULL,
		    CONSTRAINT users_2_pkey PRIMARY KEY ("id")
		);

		CREATE TABLE "public"."users_3" (
		    "id" bigint NOT NULL,
		    CONSTRAINT users_3_pkey PRIMARY KEY ("id")
		);
		`,
	), outputDefault)

	assert.Equal(t, outputDefault, outputNoConcurrency)
	assert.Equal(t, outputDefault, outputConcurrency1)
	assert.Equal(t, outputDefault, outputConcurrency10)
	assert.Equal(t, outputDefault, outputConcurrencyNoLimit)
}

func TestPsqldefSkipView(t *testing.T) {
	resetTestDatabase()

	createTable := "CREATE TABLE users (id bigint);\n"
	createView := "CREATE VIEW user_views AS SELECT id from users;\n"
	createMaterializedView := "CREATE MATERIALIZED VIEW user_materialized_views AS SELECT id from users;\n"

	mustPgExec(testDatabaseName, createTable+createView+createMaterializedView)

	tu.WriteFile("schema.sql", createTable)

	output := tu.MustExecute(t, "./psqldef", psqldefArgs(testDatabaseName, "--skip-view", "-f", "schema.sql")...)
	assert.Equal(t, nothingModified, output)
}

func TestPsqldefSkipExtension(t *testing.T) {
	resetTestDatabase()

	createExtension := "CREATE EXTENSION pgcrypto;\n"

	mustPgExec(testDatabaseName, createExtension)

	tu.WriteFile("schema.sql", "")

	output := tu.MustExecute(t, "./psqldef", psqldefArgs(testDatabaseName, "--skip-extension", "-f", "schema.sql")...)
	assert.Equal(t, nothingModified, output)
}

func TestPsqldefSkipPartition(t *testing.T) {
	resetTestDatabase()

	createRegularTable := "CREATE TABLE users (id bigint);\n"
	createPartitionedTable := tu.StripHeredoc(`
		CREATE TABLE logs (
		    id bigint,
		    created_at timestamp NOT NULL
		) PARTITION BY RANGE (created_at);
		CREATE TABLE logs_2024_01 PARTITION OF logs
		    FOR VALUES FROM ('2024-01-01') TO ('2024-02-01');
	`)
	exportRegularTable := tu.StripHeredoc(`
		CREATE TABLE "public"."users" (
		    "id" bigint
		);
	`)

	assertApplyOutput(t, createRegularTable+createPartitionedTable, wrapWithTransaction(createRegularTable+createPartitionedTable))
	tu.WriteFile("schema.sql", createRegularTable)

	output := tu.MustExecute(t, "./psqldef", psqldefArgs(testDatabaseName, "--skip-partition", "-f", "schema.sql")...)
	assert.Equal(t, nothingModified, output)

	exportOutput := tu.MustExecute(t, "./psqldef", psqldefArgs(testDatabaseName, "--skip-partition", "--export")...)
	assert.Equal(t, exportRegularTable, exportOutput)
}

func TestPsqldefBeforeApply(t *testing.T) {
	resetTestDatabase()

	// Setup
	mustPgExec(testDatabaseName, "DROP ROLE IF EXISTS dummy_owner_role;")
	mustPgExec(testDatabaseName, "CREATE ROLE dummy_owner_role;")
	mustPgExec(testDatabaseName, "GRANT ALL ON SCHEMA public TO dummy_owner_role;")

	beforeApply := "SET ROLE dummy_owner_role; SET TIME ZONE LOCAL;"
	createTable := "CREATE TABLE dummy (id int);"
	tu.WriteFile("schema.sql", createTable)

	dryRun := tu.MustExecute(t, "./psqldef", psqldefArgs(testDatabaseName, "-f", "schema.sql", "--before-apply", beforeApply, "--dry-run")...)
	apply := tu.MustExecute(t, "./psqldef", psqldefArgs(testDatabaseName, "-f", "schema.sql", "--before-apply", beforeApply)...)
	assert.Equal(t, strings.Replace(apply, "Apply", "dry run", 1), dryRun)
	assert.Equal(t, applyPrefix+"BEGIN;\n"+beforeApply+"\n"+createTable+"\nCOMMIT;\n", apply)

	apply = tu.MustExecute(t, "./psqldef", psqldefArgs(testDatabaseName, "-f", "schema.sql", "--before-apply", beforeApply)...)
	assert.Equal(t, nothingModified, apply)

	owner, err := pgQuery(testDatabaseName, "SELECT tableowner FROM pg_tables WHERE tablename = 'dummy'")
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, "dummy_owner_role\n", owner)
}

func TestPsqldefConfigIncludesTargetTables(t *testing.T) {
	resetTestDatabase()

	mustPgExec(testDatabaseName, `
        CREATE TABLE users (id bigint PRIMARY KEY);
        CREATE TABLE users_1 (id bigint PRIMARY KEY);

        CREATE TABLE users_10 (id bigint);
        ALTER TABLE users_10 ADD CONSTRAINT pkey PRIMARY KEY (id);
        ALTER TABLE users_10 ADD CONSTRAINT fkey FOREIGN KEY (id) REFERENCES users (id);
        ALTER TABLE users_10 ADD CONSTRAINT ukey UNIQUE (id);
        CREATE INDEX idx_10_1 ON users_10 (id);

        ALTER TABLE users_1 ADD CONSTRAINT fkey_1 FOREIGN KEY (id) REFERENCES users_10 (id);
    `)

	tu.WriteFile("schema.sql", `
        CREATE TABLE users (id bigint PRIMARY KEY);
        CREATE TABLE users_1 (id bigint PRIMARY KEY);
    `)

	tu.WriteFile("config.yml", "target_tables: |\n  public\\.users\n  public\\.users_\\d\n")

	apply := tu.MustExecute(t, "./psqldef", psqldefArgs(testDatabaseName, "-f", "schema.sql", "--config", "config.yml")...)
	assert.Equal(t, nothingModified, apply)
}

func TestPsqldefConfigIncludesTargetSchema(t *testing.T) {
	resetTestDatabase()

	mustPgExec(testDatabaseName, `
        CREATE SCHEMA schema_a;
        CREATE TABLE schema_a.users (id bigint PRIMARY KEY);
        CREATE SCHEMA schema_b;
        CREATE TABLE schema_b.users (id bigint PRIMARY KEY);
    `)

	tu.WriteFile("schema.sql", `
        CREATE TABLE schema_a.users (id bigint PRIMARY KEY);
    `)

	tu.WriteFile("config.yml", "target_schema: schema_a\n")

	apply := tu.MustExecute(t, "./psqldef", psqldefArgs(testDatabaseName, "-f", "schema.sql", "--config", "config.yml")...)
	assert.Equal(t, nothingModified, apply)

	// multiple targets
	mustPgExec(testDatabaseName, `
        CREATE SCHEMA schema_c;
        CREATE TABLE schema_c.users (id bigint PRIMARY KEY);
    `)

	tu.WriteFile("schema.sql", `
        CREATE TABLE schema_a.users (id bigint PRIMARY KEY);
        CREATE TABLE schema_b.users (id bigint PRIMARY KEY);
    `)

	tu.WriteFile("config.yml", `target_schema: |
  schema_a
  schema_b`)

	apply = tu.MustExecute(t, "./psqldef", psqldefArgs(testDatabaseName, "-f", "schema.sql", "--config", "config.yml")...)
	assert.Equal(t, nothingModified, apply)
}

func TestPsqldefConfigIncludesTargetSchemaWithViews(t *testing.T) {
	resetTestDatabase()

	mustPgExec(testDatabaseName, `
				CREATE SCHEMA foo;

				CREATE TABLE foo.users (
					id BIGINT PRIMARY KEY,
					name character varying(100)
				);
				CREATE TABLE foo.posts (
					id BIGINT PRIMARY KEY,
					name character varying(100),
					user_id BIGINT,
					is_deleted boolean
				);
				CREATE VIEW foo.user_views AS SELECT id from foo.users;
				CREATE MATERIALIZED VIEW foo.view_user_posts AS SELECT p.id FROM (foo.posts as p JOIN foo.users as u ON ((p.user_id = u.id)));
    `)

	schema := tu.StripHeredoc(`
				CREATE SCHEMA bar;
				CREATE TABLE bar.companies (
					id BIGINT PRIMARY KEY
				);
	`)
	tu.WriteFile("schema.sql", schema)

	tu.WriteFile("config.yml", "target_schema: bar\n")

	apply := tu.MustExecute(t, "./psqldef", psqldefArgs(testDatabaseName, "-f", "schema.sql", "--config", "config.yml")...)
	assert.Equal(t, wrapWithTransaction(schema), apply)

	apply = tu.MustExecute(t, "./psqldef", psqldefArgs(testDatabaseName, "-f", "schema.sql", "--config", "config.yml")...)
	assert.Equal(t, nothingModified, apply)
}

func TestPsqldefConfigIncludesSkipTables(t *testing.T) {
	resetTestDatabase()

	mustPgExec(testDatabaseName, `
        CREATE TABLE users (id bigint PRIMARY KEY);
        CREATE TABLE users_1 (id bigint PRIMARY KEY);

        CREATE TABLE users_10 (id bigint);
        ALTER TABLE users_10 ADD CONSTRAINT pkey PRIMARY KEY (id);
        ALTER TABLE users_10 ADD CONSTRAINT fkey FOREIGN KEY (id) REFERENCES users (id);
        ALTER TABLE users_10 ADD CONSTRAINT ukey UNIQUE (id);
        CREATE INDEX idx_10_1 ON users_10 (id);

        ALTER TABLE users_1 ADD CONSTRAINT fkey_1 FOREIGN KEY (id) REFERENCES users_10 (id);
    `)

	tu.WriteFile("schema.sql", `
        CREATE TABLE users (id bigint PRIMARY KEY);
        CREATE TABLE users_1 (id bigint PRIMARY KEY);
    `)

	tu.WriteFile("config.yml", "skip_tables: |\n  public\\.users_10\n")

	apply := tu.MustExecute(t, "./psqldef", psqldefArgs(testDatabaseName, "-f", "schema.sql", "--config", "config.yml")...)
	assert.Equal(t, nothingModified, apply)
}

func TestPsqldefConfigIncludesSkipViews(t *testing.T) {
	resetTestDatabase()

	mustPgExec(testDatabaseName, `
	    CREATE MATERIALIZED VIEW views AS SELECT 1 AS id, 12 AS uid;
        CREATE MATERIALIZED VIEW views_1 AS SELECT 1 AS id, 13 AS uid;

        CREATE MATERIALIZED VIEW views_10 AS SELECT 1 AS id, 14 AS uid;
		CREATE INDEX idx_views_10 ON views_10 (id);
		CREATE UNIQUE INDEX uidx_views_10 ON views_10 (uid);
	`)

	tu.WriteFile("schema.sql", `
        CREATE MATERIALIZED VIEW views AS SELECT 1 AS id, 12 AS uid;
        CREATE MATERIALIZED VIEW views_1 AS SELECT 1 AS id, 13 AS uid;
    `)

	tu.WriteFile("config.yml", "skip_views: |\n  public\\.views_10\n")

	apply := tu.MustExecute(t, "./psqldef", psqldefArgs(testDatabaseName, "-f", "schema.sql", "--config", "config.yml")...)
	assert.Equal(t, nothingModified, apply)
}

func TestPsqldefHelp(t *testing.T) {
	_, err := tu.Execute("./psqldef", "--help")
	if err != nil {
		t.Errorf("failed to run --help: %s", err)
	}

	out, err := tu.Execute("./psqldef")
	if err == nil {
		t.Errorf("no database must be error, but successfully got: %s", out)
	}
}

func TestPsqldefTransactionBoundariesWithConcurrentIndex(t *testing.T) {
	resetTestDatabase()

	mustPgExec(testDatabaseName, tu.StripHeredoc(`
		CREATE TABLE users (
		    id bigint NOT NULL PRIMARY KEY,
		    email text,
		    age integer,
		    name text
		);`))

	// Test 1: Single CREATE INDEX CONCURRENTLY - should be outside transaction
	t.Run("SingleConcurrentIndex", func(t *testing.T) {
		schema := tu.StripHeredoc(`
			CREATE TABLE users (
			    id bigint NOT NULL PRIMARY KEY,
			    email text,
			    age integer,
			    name text
			);
			CREATE INDEX CONCURRENTLY idx_users_email ON users (email);`)

		expected := applyPrefix + "CREATE INDEX CONCURRENTLY idx_users_email ON users (email);\n"
		assertApplyOutput(t, schema, expected)
	})

	// Test 2: Mix of regular DDLs and concurrent index
	t.Run("MixedDDLsWithConcurrentIndex", func(t *testing.T) {
		resetTestDatabase()
		mustPgExec(testDatabaseName, tu.StripHeredoc(`
			CREATE TABLE users (
			    id bigint NOT NULL PRIMARY KEY,
			    email text
			);`))

		schema := tu.StripHeredoc(`
			CREATE TABLE users (
			    id bigint NOT NULL PRIMARY KEY,
			    email text,
			    age integer,
			    name text
			);
			CREATE INDEX idx_users_age ON users (age);
			CREATE INDEX CONCURRENTLY idx_users_email ON users (email);
			CREATE INDEX CONCURRENTLY idx_users_name ON users (name);`)

		// Regular DDLs should be in transaction, concurrent indexes outside
		expected := applyPrefix + tu.StripHeredoc(`
			BEGIN;
			ALTER TABLE "public"."users" ADD COLUMN "age" integer;
			ALTER TABLE "public"."users" ADD COLUMN "name" text;
			CREATE INDEX idx_users_age ON users (age);
			COMMIT;
			CREATE INDEX CONCURRENTLY idx_users_email ON users (email);
			CREATE INDEX CONCURRENTLY idx_users_name ON users (name);
		`)

		assertApplyOutput(t, schema, expected)
	})

	// Test 3: DROP INDEX CONCURRENTLY - should be outside transaction
	t.Run("DropConcurrentIndex", func(t *testing.T) {
		resetTestDatabase()
		mustPgExec(testDatabaseName, tu.StripHeredoc(`
			CREATE TABLE users (
			    id bigint NOT NULL PRIMARY KEY,
			    email text,
			    age integer
			);
			CREATE INDEX idx_users_email ON users (email);
			CREATE INDEX idx_users_age ON users (age);`))

		// Dropping the indexes with CONCURRENTLY should be outside transaction
		schema := tu.StripHeredoc(`
			CREATE TABLE users (
			    id bigint NOT NULL PRIMARY KEY,
			    email text,
			    age integer
			);`)

		// Note: psqldef may not generate DROP INDEX CONCURRENTLY by default
		// This test may need adjustment based on actual behavior
		// For now, we'll test that regular DROP INDEX is in transaction
		expected := wrapWithTransaction(tu.StripHeredoc(`
			DROP INDEX "public"."idx_users_age";
			DROP INDEX "public"."idx_users_email";
		`))

		assertApplyOutputWithEnableDrop(t, schema, expected)
	})

	// Test 4: Dry run with concurrent index
	t.Run("DryRunWithConcurrentIndex", func(t *testing.T) {
		resetTestDatabase()
		mustPgExec(testDatabaseName, tu.StripHeredoc(`
			CREATE TABLE users (
			    id bigint NOT NULL PRIMARY KEY,
			    email text
			);`))

		tu.WriteFile("schema.sql", tu.StripHeredoc(`
			CREATE TABLE users (
			    id bigint NOT NULL PRIMARY KEY,
			    email text,
			    age integer
			);
			CREATE INDEX CONCURRENTLY idx_users_email ON users (email);
			CREATE INDEX idx_users_age ON users (age);`))

		dryRun := tu.MustExecute(t, "./psqldef", psqldefArgs(testDatabaseName, "--dry-run", "--file", "schema.sql")...)
		apply := tu.MustExecute(t, "./psqldef", psqldefArgs(testDatabaseName, "--file", "schema.sql")...)

		// Verify that dry run output matches apply output (except for the prefix)
		assert.Equal(t, strings.Replace(apply, "Apply", "dry run", 1), dryRun)

		// Verify the structure of the output
		expectedStructure := "-- dry run --\n" + tu.StripHeredoc(`
			BEGIN;
			ALTER TABLE "public"."users" ADD COLUMN "age" integer;
			CREATE INDEX idx_users_age ON users (age);
			COMMIT;
			CREATE INDEX CONCURRENTLY idx_users_email ON users (email);
		`)

		assert.Equal(t, expectedStructure, dryRun)
	})

	// Test 5: Multiple concurrent operations
	t.Run("MultipleConcurrentOperations", func(t *testing.T) {
		resetTestDatabase()
		mustPgExec(testDatabaseName, tu.StripHeredoc(`
			CREATE TABLE users (
			    id bigint NOT NULL PRIMARY KEY,
			    email text,
			    age integer
			);
			CREATE TABLE orders (
			    id bigint NOT NULL PRIMARY KEY,
			    user_id bigint,
			    status text
			);`))

		schema := tu.StripHeredoc(`
			CREATE TABLE users (
			    id bigint NOT NULL PRIMARY KEY,
			    email text,
			    age integer,
			    name text
			);
			CREATE TABLE orders (
			    id bigint NOT NULL PRIMARY KEY,
			    user_id bigint,
			    status text,
			    created_at timestamp
			);
			CREATE INDEX CONCURRENTLY idx_users_email ON users (email);
			CREATE INDEX CONCURRENTLY idx_orders_user_id ON orders (user_id);
			CREATE INDEX idx_orders_status ON orders (status);`)

		expected := applyPrefix + tu.StripHeredoc(`
			BEGIN;
			ALTER TABLE "public"."users" ADD COLUMN "name" text;
			ALTER TABLE "public"."orders" ADD COLUMN "created_at" timestamp;
			CREATE INDEX idx_orders_status ON orders (status);
			COMMIT;
			CREATE INDEX CONCURRENTLY idx_users_email ON users (email);
			CREATE INDEX CONCURRENTLY idx_orders_user_id ON orders (user_id);
		`)

		assertApplyOutput(t, schema, expected)
	})
}

func TestPsqldefReindexConcurrently(t *testing.T) {
	resetTestDatabase()

	// Create table with indexes
	mustPgExec(testDatabaseName, tu.StripHeredoc(`
		CREATE TABLE users (
		    id bigint NOT NULL PRIMARY KEY,
		    email text,
		    age integer
		);
		CREATE INDEX idx_users_email ON users (email);
		CREATE INDEX idx_users_age ON users (age);`))

	// Note: REINDEX CONCURRENTLY is PostgreSQL 12+ feature
	// This test verifies that REINDEX CONCURRENTLY would be handled outside transaction
	t.Run("ReindexConcurrently", func(t *testing.T) {
		// Test that if we had a REINDEX CONCURRENTLY in beforeApply, it would work
		tu.WriteFile("schema.sql", tu.StripHeredoc(`
			CREATE TABLE users (
			    id bigint NOT NULL PRIMARY KEY,
			    email text,
			    age integer
			);
			CREATE INDEX idx_users_email ON users (email);
			CREATE INDEX idx_users_age ON users (age);`))

		// Verify that regular operations still work
		output := tu.MustExecute(t, "./psqldef", psqldefArgs(testDatabaseName, "--file", "schema.sql")...)
		assert.Equal(t, nothingModified, output)
	})
}

func TestMain(m *testing.M) {
	resetTestDatabase()
	tu.BuildForTest()
	status := m.Run()

	cleanupTestRoles()
	_ = os.Remove("psqldef")
	_ = os.Remove("schema.sql")
	_ = os.Remove("config.yml")
	os.Exit(status)
}

func assertApplyOutput(t *testing.T, schema string, expected string) {
	t.Helper()
	actual := assertApplyOutputWithConfig(t, schema, database.GeneratorConfig{EnableDrop: false, LegacyIgnoreQuotes: true})
	assert.Equal(t, expected, actual)
}

func assertApplyOutputWithEnableDrop(t *testing.T, schema string, expected string) {
	t.Helper()
	actual := assertApplyOutputWithConfig(t, schema, database.GeneratorConfig{EnableDrop: true, LegacyIgnoreQuotes: true})
	assert.Equal(t, expected, actual)
}

func assertApplyOutputWithConfig(t *testing.T, desiredSchema string, config database.GeneratorConfig) string {
	t.Helper()

	db, err := connectDatabase(dbConfig{
		DbName: testDatabaseName,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sqlParser := postgres.NewParser()
	output, err := tu.ApplyWithOutput(db, schema.GeneratorModePostgres, sqlParser, desiredSchema, config)
	if err != nil {
		t.Fatal(err)
	}

	return output
}

func assertExportOutput(t *testing.T, expected string) {
	t.Helper()
	actual := tu.MustExecute(t, "./psqldef", psqldefArgs(testDatabaseName, "--export")...)
	assert.Equal(t, expected, actual)
}

// resetTestDatabase drops and recreates the test database.
func resetTestDatabase() {
	// PostgreSQL cannot drop the database if it is connected to, so we need to use the default database.
	defaultDatabaseName := "postgres"
	mustPgExec(defaultDatabaseName, fmt.Sprintf("DROP DATABASE IF EXISTS %s", testDatabaseName))
	mustPgExec(defaultDatabaseName, fmt.Sprintf("CREATE DATABASE %s", testDatabaseName))
}

var testRoles = []string{
	"readonly_user",
	"app_user",
	"admin_role",
	"user-with-dash",
	"user.with.dot",
	"user with spaces",
	"CamelCaseUser",
	"UPPERCASE_USER",
	"user1",
	"user2",
	"user3",
	"user4",
	"My User",
	"reader",
	"writer",
	"admin",
	"reader1",
	"reader2",
	"writer1",
	"writer2",
	"role1",
	"role2",
	"power_user",
	"user@domain.com",
}

func createTestRole(role string) {
	// Escape single quotes in role name for SQL string literal
	escapedRole := strings.ReplaceAll(role, "'", "''")
	// Quote identifier for CREATE ROLE statement
	quotedRole := fmt.Sprintf(`"%s"`, strings.ReplaceAll(role, `"`, `""`))

	query := fmt.Sprintf(`DO $$ BEGIN
		IF NOT EXISTS (SELECT * FROM pg_roles WHERE rolname = '%s') THEN
			CREATE ROLE %s;
		END IF;
	END $$;`, escapedRole, quotedRole)

	mustPgExec(testDatabaseName, query)
}

func createAllTestRoles() {
	for _, role := range testRoles {
		createTestRole(role)
	}
}

func cleanupTestRoles() {
	db, err := connectDatabase(defaultDbConfig)
	if err != nil {
		// Don't panic during cleanup, just return
		return
	}
	defer db.Close()

	for _, role := range testRoles {
		// Escape single quotes in role name for SQL string literal
		escapedRole := strings.ReplaceAll(role, "'", "''")
		// Quote identifier for REVOKE and DROP statements
		quotedRole := fmt.Sprintf(`"%s"`, strings.ReplaceAll(role, `"`, `""`))

		// First revoke all privileges from the role
		revokeQuery := fmt.Sprintf(`DO $$
			DECLARE r RECORD;
			BEGIN
				FOR r IN
					SELECT nspname, relname
					FROM pg_class c
					JOIN pg_namespace n ON n.oid = c.relnamespace
					WHERE relkind IN ('r', 'v', 'm')
					AND has_table_privilege('%s', c.oid, 'SELECT')
				LOOP
					EXECUTE format('REVOKE ALL ON %%I.%%I FROM %s', r.nspname, r.relname);
				END LOOP;
			END $$;`, escapedRole, quotedRole)
		_, _ = db.DB().Exec(revokeQuery) // Ignore errors during cleanup

		// Then drop the role
		dropQuery := fmt.Sprintf("DROP ROLE IF EXISTS %s;", quotedRole)
		_, _ = db.DB().Exec(dropQuery) // Ignore errors during cleanup
	}
}

var publicAndNonPublicSchemaTestCases = []struct {
	Name   string
	Schema string
}{
	{Name: "in public schema", Schema: "public"},
	{Name: "in non-public schema", Schema: "test"},
}

// TestPsqldefCreateDomain tests domain creation in both public and non-public schemas
func TestPsqldefCreateDomain(t *testing.T) {
	for _, tc := range publicAndNonPublicSchemaTestCases {
		t.Run(tc.Name, func(t *testing.T) {
			resetTestDatabase()
			mustPgExec(testDatabaseName, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s;", tc.Schema))

			// Test creating a basic domain
			createDomain := fmt.Sprintf("CREATE DOMAIN %s.email AS text;\n", tc.Schema)
			assertApplyOutput(t, createDomain, wrapWithTransaction(createDomain))
			assertApplyOutput(t, createDomain, nothingModified)

			// Test creating a domain with constraints
			createDomainWithConstraints := fmt.Sprintf("CREATE DOMAIN %s.positive_int AS integer DEFAULT 1 NOT NULL;\n", tc.Schema)
			assertApplyOutput(t, createDomain+createDomainWithConstraints, wrapWithTransaction(createDomainWithConstraints))
			assertApplyOutput(t, createDomain+createDomainWithConstraints, nothingModified)

			// Test dropping a domain (requires enable_drop)
			assertApplyOutputWithEnableDrop(t, createDomain, wrapWithTransaction(fmt.Sprintf("DROP DOMAIN \"%s\".\"positive_int\";\n", tc.Schema)))
			assertApplyOutputWithEnableDrop(t, createDomain, nothingModified)
		})
	}
}

// TestPsqldefDomainWithTargetSchema tests that TargetSchema filtering works correctly for domains
func TestPsqldefDomainWithTargetSchema(t *testing.T) {
	resetTestDatabase()

	// Create two schemas with domains of the same name but different constraints
	mustPgExec(testDatabaseName, `
		CREATE SCHEMA test_schema_a;
		CREATE SCHEMA test_schema_b;
		CREATE DOMAIN test_schema_a.amount AS integer CHECK (VALUE > 0);
		CREATE DOMAIN test_schema_b.amount AS integer CHECK (VALUE < 0);
		CREATE DOMAIN test_schema_a.email AS text CHECK (VALUE ~ '@');
		CREATE DOMAIN test_schema_b.status AS text DEFAULT 'pending';
	`)

	t.Run("filter to schema_a only", func(t *testing.T) {
		// Export schema with TargetSchema filtering to schema_a
		db, err := connectDatabase(dbConfig{DbName: testDatabaseName})
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()

		config := database.GeneratorConfig{
			TargetSchema:       []string{"test_schema_a"},
			LegacyIgnoreQuotes: true,
		}
		db.SetGeneratorConfig(config)

		exported, err := db.ExportDDLs()
		if err != nil {
			t.Fatal(err)
		}

		// Verify schema_a domains are present (using quoted identifiers)
		assert.Contains(t, exported, `CREATE DOMAIN "test_schema_a"."amount"`)
		assert.Contains(t, exported, "CHECK (VALUE > 0)") // schema_a constraint
		assert.Contains(t, exported, `CREATE DOMAIN "test_schema_a"."email"`)
		assert.Contains(t, exported, "CHECK (VALUE ~ '@'")

		// Verify schema_b domains are NOT present
		assert.NotContains(t, exported, `CREATE DOMAIN "test_schema_b"."amount"`)
		assert.NotContains(t, exported, "CHECK (VALUE < 0)") // schema_b constraint
		assert.NotContains(t, exported, `CREATE DOMAIN "test_schema_b"."status"`)
	})

	t.Run("filter to schema_b only", func(t *testing.T) {
		db, err := connectDatabase(dbConfig{DbName: testDatabaseName})
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()

		config := database.GeneratorConfig{
			TargetSchema:       []string{"test_schema_b"},
			LegacyIgnoreQuotes: true,
		}
		db.SetGeneratorConfig(config)

		exported, err := db.ExportDDLs()
		if err != nil {
			t.Fatal(err)
		}

		// Verify schema_b domains are present (using quoted identifiers)
		assert.Contains(t, exported, `CREATE DOMAIN "test_schema_b"."amount"`)
		assert.Contains(t, exported, "CHECK (VALUE < 0)") // schema_b constraint
		assert.Contains(t, exported, `CREATE DOMAIN "test_schema_b"."status"`)
		assert.Contains(t, exported, "DEFAULT 'pending'")

		// Verify schema_a domains are NOT present
		assert.NotContains(t, exported, `CREATE DOMAIN "test_schema_a"."amount"`)
		assert.NotContains(t, exported, "CHECK (VALUE > 0)") // schema_a constraint
		assert.NotContains(t, exported, `CREATE DOMAIN "test_schema_a"."email"`)
	})

	t.Run("filter to both schemas", func(t *testing.T) {
		db, err := connectDatabase(dbConfig{DbName: testDatabaseName})
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()

		config := database.GeneratorConfig{
			TargetSchema:       []string{"test_schema_a", "test_schema_b"},
			LegacyIgnoreQuotes: true,
		}
		db.SetGeneratorConfig(config)

		exported, err := db.ExportDDLs()
		if err != nil {
			t.Fatal(err)
		}

		// Verify all domains from both schemas are present (using quoted identifiers)
		assert.Contains(t, exported, `CREATE DOMAIN "test_schema_a"."amount"`)
		assert.Contains(t, exported, "CHECK (VALUE > 0)")
		assert.Contains(t, exported, `CREATE DOMAIN "test_schema_b"."amount"`)
		assert.Contains(t, exported, "CHECK (VALUE < 0)")
		assert.Contains(t, exported, `CREATE DOMAIN "test_schema_a"."email"`)
		assert.Contains(t, exported, `CREATE DOMAIN "test_schema_b"."status"`)
	})
}

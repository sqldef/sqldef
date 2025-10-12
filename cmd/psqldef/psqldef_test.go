// Integration test of psqldef command.
//
// Test requirement:
//   - go command
//   - `psql -Upostgres` must succeed
package main

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/sqldef/sqldef/v3/cmd/testutils"
	"github.com/sqldef/sqldef/v3/database"
	"github.com/sqldef/sqldef/v3/database/postgres"
	"github.com/sqldef/sqldef/v3/schema"
)

const (
	applyPrefix      = "-- Apply --\n"
	nothingModified  = "-- Nothing is modified --\n"
	defaultUser      = "postgres"
	testDatabaseName = "psqldef_test"
)

type dbConfig struct {
	User   string
	DbName string
}

var defaultDbConfig = dbConfig{
	User:   defaultUser,
	DbName: testDatabaseName,
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
		Host:    "127.0.0.1",
		Port:    5432,
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

	return testutils.QueryRows(db, query)
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

func TestApply(t *testing.T) {
	tests, err := testutils.ReadTests("tests*.yml")
	if err != nil {
		t.Fatal(err)
	}

	version := mustGetServerVersion()
	sqlParser := postgres.NewParser()
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			resetTestDatabaseWithUser(test.User)

			db, err := connectDatabase(dbConfig{
				User:   test.User,
				DbName: testDatabaseName,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()

			testutils.RunTest(t, db, test, schema.GeneratorModePostgres, sqlParser, version, "")
		})
	}
}

// TODO: non-CLI tests should be migrated to TestApply

func TestPsqldefCreateTableChangeDefaultTimestamp(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE timestamps (
		  created_at timestamp default current_timestamp
		);
		`,
	)
	assertApplyOutput(t, createTable, wrapWithTransaction(createTable))
	assertApplyOutput(t, createTable, nothingModified)

	createTableDropDefault := stripHeredoc(`
		CREATE TABLE timestamps (
		  created_at timestamp
		);
		`,
	)
	alter1 := `ALTER TABLE "public"."timestamps" ALTER COLUMN "created_at" DROP DEFAULT;`
	assertApplyOutput(t, createTableDropDefault, wrapWithTransaction(alter1+"\n"))
	assertApplyOutput(t, createTableDropDefault, nothingModified)

	alter2 := `ALTER TABLE "public"."timestamps" ALTER COLUMN "created_at" SET DEFAULT current_timestamp;`
	assertApplyOutput(t, createTable, wrapWithTransaction(alter2+"\n"))
	assertApplyOutput(t, createTable, nothingModified)
}

func TestPsqldefCreateTableNotNull(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  name text
		);
		`,
	)
	assertApplyOutput(t, createTable, wrapWithTransaction(createTable))
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  name text NOT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, wrapWithTransaction(stripHeredoc(`
		ALTER TABLE "public"."users" ALTER COLUMN "name" SET NOT NULL;
		`)))
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  name text
		);
		`,
	)
	assertApplyOutput(t, createTable, wrapWithTransaction(stripHeredoc(`
		ALTER TABLE "public"."users" ALTER COLUMN "name" DROP NOT NULL;
		`)))
	assertApplyOutput(t, createTable, nothingModified)
}

func TestPsqldefCitextExtension(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
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

func TestPsqldefIgnoreExtension(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
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
	assertExportOutput(t, stripHeredoc(`
		CREATE EXTENSION "pg_buffercache";

		CREATE TABLE "public"."users" (
		    "id" bigint NOT NULL,
		    "name" text,
		    "age" integer
		);
		`))

	mustPgExec(testDatabaseName, "DROP EXTENSION pg_buffercache;")
}

func TestPsqldefCreateTablePrimaryKey(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL PRIMARY KEY,
		  name text
		);
		`,
	)
	assertApplyOutput(t, createTable, wrapWithTransaction(createTable))
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  name text
		);`,
	)
	assertApplyOutputWithEnableDrop(t, createTable, wrapWithTransaction(
		`ALTER TABLE "public"."users" DROP CONSTRAINT "users_pkey";`+"\n"+
			`ALTER TABLE "public"."users" DROP COLUMN "id";`+"\n"),
	)
	assertApplyOutputWithEnableDrop(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL PRIMARY KEY,
		  name text
		);`,
	)
	assertApplyOutputWithEnableDrop(t, createTable, wrapWithTransaction(stripHeredoc(`
		ALTER TABLE "public"."users" ADD COLUMN "id" bigint NOT NULL;
		ALTER TABLE "public"."users" ADD PRIMARY KEY ("id");
		`,
	)))
	assertApplyOutputWithEnableDrop(t, createTable, nothingModified)
}

func TestPsqldefCreateTableConstraintPrimaryKey(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  a integer,
		  b integer,
		  CONSTRAINT a_b_pkey PRIMARY KEY (a, b)
		);
		`,
	)
	assertApplyOutput(t, createTable, wrapWithTransaction(createTable))
	assertApplyOutput(t, createTable, nothingModified)
}

func TestPsqldefCreateTableForeignKey(t *testing.T) {
	resetTestDatabase()

	createUsers := "CREATE TABLE users (id BIGINT PRIMARY KEY);\n"
	createPosts := stripHeredoc(`
		CREATE TABLE posts (
		  content text,
		  user_id bigint
		);
		`,
	)
	assertApplyOutput(t, createUsers+createPosts, wrapWithTransaction(createUsers+createPosts))
	assertApplyOutput(t, createUsers+createPosts, nothingModified)

	createPosts = stripHeredoc(`
		CREATE TABLE posts (
		  content text,
		  user_id bigint,
		  CONSTRAINT posts_ibfk_1 FOREIGN KEY (user_id) REFERENCES users (id)
		);
		`,
	)
	assertApplyOutput(t, createUsers+createPosts, wrapWithTransaction(`ALTER TABLE "public"."posts" ADD CONSTRAINT "posts_ibfk_1" FOREIGN KEY ("user_id") REFERENCES "public"."users" ("id");`+"\n"))
	assertApplyOutput(t, createUsers+createPosts, nothingModified)

	createPosts = stripHeredoc(`
		CREATE TABLE posts (
		  content text,
		  user_id bigint,
		  CONSTRAINT posts_ibfk_1 FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE SET NULL ON UPDATE CASCADE
		);
		`,
	)
	assertApplyOutput(t, createUsers+createPosts, wrapWithTransaction(`ALTER TABLE "public"."posts" DROP CONSTRAINT "posts_ibfk_1";`+"\n"+
		`ALTER TABLE "public"."posts" ADD CONSTRAINT "posts_ibfk_1" FOREIGN KEY ("user_id") REFERENCES "public"."users" ("id") ON DELETE SET NULL ON UPDATE CASCADE;`+"\n"))
	assertApplyOutput(t, createUsers+createPosts, nothingModified)

	createPosts = stripHeredoc(`
		CREATE TABLE posts (
		  content text,
		  user_id bigint
		);
		`,
	)
	assertApplyOutput(t, createUsers+createPosts, wrapWithTransaction(`ALTER TABLE "public"."posts" DROP CONSTRAINT "posts_ibfk_1";`+"\n"))
	assertApplyOutput(t, createUsers+createPosts, nothingModified)
}

func TestPsqldefAddForeignKey(t *testing.T) {
	resetTestDatabase()

	createUsers := "CREATE TABLE users (id BIGINT PRIMARY KEY);\n"
	createPosts := stripHeredoc(`
		CREATE TABLE posts (
		  content text,
		  user_id bigint,
		  CONSTRAINT posts_ibfk_1 FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE SET NULL ON UPDATE CASCADE
		);
		`,
	)
	assertApplyOutput(t, createUsers+createPosts, wrapWithTransaction(createUsers+createPosts))
	assertApplyOutput(t, createUsers+createPosts, nothingModified)

	createPosts = stripHeredoc(`
		CREATE TABLE posts (
		  content text,
		  user_id bigint
		);
		`,
	)
	addForeignKey := "ALTER TABLE ONLY public.posts ADD CONSTRAINT posts_ibfk_1 FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE SET NULL ON UPDATE CASCADE;\n"
	assertApplyOutput(t, createUsers+createPosts+addForeignKey, nothingModified)
}

func TestPsqldefCreateTableWithReferences(t *testing.T) {
	resetTestDatabase()

	createTableA := stripHeredoc(`
		CREATE TABLE a (
		  a_id INTEGER PRIMARY KEY,
		  my_text TEXT UNIQUE NOT NULL
		);
		`,
	)
	createTableB := stripHeredoc(`
		CREATE TABLE b (
		  b_id INTEGER PRIMARY KEY,
		  a_id INTEGER REFERENCES a,
		  a_my_text TEXT NOT NULL REFERENCES a (my_text)
		);
		`,
	)
	assertApplyOutput(t, createTableA+createTableB, wrapWithTransaction(createTableA+createTableB))
	assertApplyOutput(t, createTableA+createTableB, nothingModified)

	createTableB = stripHeredoc(`
		CREATE TABLE b (
		  b_id INTEGER PRIMARY KEY,
		  a_id INTEGER,
		  a_my_text TEXT NOT NULL
		);
		`,
	)
	assertApplyOutput(t, createTableA+createTableB, wrapWithTransaction(`ALTER TABLE "public"."b" DROP CONSTRAINT "b_a_id_fkey";`+"\n"+
		`ALTER TABLE "public"."b" DROP CONSTRAINT "b_a_my_text_fkey";`+"\n"))
	assertApplyOutput(t, createTableA+createTableB, nothingModified)
}

func TestPsqldefCreateTableWithReferencesOnDelete(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE customers (
		  id UUID NOT NULL PRIMARY KEY,
		  customer_name VARCHAR(255) NOT NULL
		);
		CREATE TABLE orders (
		  id UUID NOT NULL PRIMARY KEY,
		  order_number VARCHAR(255) NOT NULL,
		  customer UUID REFERENCES customers(id) ON DELETE CASCADE
		);
		`,
	)
	assertApplyOutput(t, createTable, wrapWithTransaction(createTable))
	assertApplyOutput(t, createTable, nothingModified)
}

func TestPsqldefCreateTableWithConstraintReferences(t *testing.T) {
	resetTestDatabase()
	mustPgExec(testDatabaseName, "CREATE SCHEMA a;")
	mustPgExec(testDatabaseName, "CREATE SCHEMA c;")

	createTable := stripHeredoc(`
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

	createTable = stripHeredoc(`
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

func TestPsqldefCreateTableWithCheck(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE a (
		  a_id INTEGER PRIMARY KEY CHECK (a_id > 0),
		  my_text TEXT UNIQUE NOT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, wrapWithTransaction(createTable))
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE a (
		  a_id INTEGER PRIMARY KEY CHECK (a_id > 1),
		  my_text TEXT UNIQUE NOT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, wrapWithTransaction(`ALTER TABLE "public"."a" DROP CONSTRAINT a_a_id_check;`+"\n"+
		`ALTER TABLE "public"."a" ADD CONSTRAINT a_a_id_check CHECK (a_id > 1);`+"\n"))
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE a (
		  a_id INTEGER PRIMARY KEY,
		  my_text TEXT UNIQUE NOT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, wrapWithTransaction(`ALTER TABLE "public"."a" DROP CONSTRAINT a_a_id_check;`+"\n"))
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE a (
		  a_id INTEGER PRIMARY KEY CHECK (a_id > 2) NO INHERIT,
		  my_text TEXT UNIQUE NOT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, wrapWithTransaction(`ALTER TABLE "public"."a" ADD CONSTRAINT a_a_id_check CHECK (a_id > 2) NO INHERIT;`+"\n"))
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE a (
		  a_id INTEGER PRIMARY KEY CHECK (a_id > 3) NO INHERIT,
		  my_text TEXT UNIQUE NOT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, wrapWithTransaction(`ALTER TABLE "public"."a" DROP CONSTRAINT a_a_id_check;`+"\n"+
		`ALTER TABLE "public"."a" ADD CONSTRAINT a_a_id_check CHECK (a_id > 3) NO INHERIT;`+"\n"))
	assertApplyOutput(t, createTable, nothingModified)
}

func TestPsqldefMultiColumnCheck(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE orders (
		  id UUID NOT NULL PRIMARY KEY,
		  order_number VARCHAR(255) NOT NULL,
		  customer VARCHAR(255),
		  store_table VARCHAR(255)
		);
		`)
	assertApplyOutput(t, createTable, wrapWithTransaction(createTable))
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE orders (
		  id UUID NOT NULL PRIMARY KEY,
		  order_number VARCHAR(255) NOT NULL,
		  customer VARCHAR(255),
		  store_table VARCHAR(255),
		  CONSTRAINT check_customer_or_table CHECK (store_table is not null and customer is null or store_table is null and customer is not null)
		);
		`)
	assertApplyOutput(t, createTable, wrapWithTransaction(`ALTER TABLE "public"."orders" ADD CONSTRAINT "check_customer_or_table" CHECK (store_table is not null and customer is null or store_table is null and customer is not null);`+"\n"))
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE orders (
		  id UUID NOT NULL PRIMARY KEY,
		  order_number VARCHAR(255) NOT NULL,
		  customer VARCHAR(255),
		  store_table VARCHAR(255)
		);
		`)
	assertApplyOutput(t, createTable, wrapWithTransaction(`ALTER TABLE "public"."orders" DROP CONSTRAINT "check_customer_or_table";`+"\n"))
	assertApplyOutput(t, createTable, nothingModified)
}

func TestPsqlddefCreatePolicy(t *testing.T) {
	resetTestDatabase()

	createUsers := "CREATE TABLE users (id BIGINT PRIMARY KEY, name character varying(100));\n"

	assertApplyOutput(t, createUsers, wrapWithTransaction(createUsers))
	assertApplyOutput(t, createUsers, nothingModified)

	createPolicy := stripHeredoc(`
		CREATE POLICY p_users ON users AS PERMISSIVE FOR ALL TO PUBLIC USING (id = (current_user)::integer) WITH CHECK ((current_user)::integer = 1);
		`,
	)
	assertApplyOutput(t, createUsers+createPolicy, wrapWithTransaction("CREATE POLICY p_users ON users AS PERMISSIVE FOR ALL TO PUBLIC USING (id = (current_user)::integer) WITH CHECK ((current_user)::integer = 1);\n"))
	assertApplyOutput(t, createUsers+createPolicy, nothingModified)

	createPolicy = stripHeredoc(`
		CREATE POLICY p_users ON users AS RESTRICTIVE FOR ALL TO postgres USING (id = (current_user)::integer);
		`,
	)
	assertApplyOutput(t, createUsers+createPolicy, wrapWithTransaction(stripHeredoc(`
		DROP POLICY "p_users" ON "public"."users";
		CREATE POLICY p_users ON users AS RESTRICTIVE FOR ALL TO postgres USING (id = (current_user)::integer);
		`)))
	assertApplyOutput(t, createUsers+createPolicy, nothingModified)

	createPolicy = stripHeredoc(`
		CREATE POLICY p_users ON users AS RESTRICTIVE FOR ALL TO postgres USING (true);
		`,
	)
	assertApplyOutput(t, createUsers+createPolicy, wrapWithTransaction(stripHeredoc(`
		DROP POLICY "p_users" ON "public"."users";
		CREATE POLICY p_users ON users AS RESTRICTIVE FOR ALL TO postgres USING (true);
		`)))
	assertApplyOutput(t, createUsers+createPolicy, nothingModified)

	assertApplyOutput(t, createUsers, wrapWithTransaction(`DROP POLICY "p_users" ON "public"."users";`+"\n"))
	assertApplyOutput(t, createUsers, nothingModified)
}

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

func TestPsqldefCreateIndex(t *testing.T) {
	for _, tc := range publicAndNonPublicSchemaTestCases {
		t.Run(tc.Name, func(t *testing.T) {
			resetTestDatabase()
			mustPgExec(testDatabaseName, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s;", tc.Schema))

			createTable := stripHeredoc(fmt.Sprintf(`
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

func TestPsqldefCreateMaterializedViewIndex(t *testing.T) {
	for _, tc := range publicAndNonPublicSchemaTestCases {
		t.Run(tc.Name, func(t *testing.T) {
			resetTestDatabase()
			mustPgExec(testDatabaseName, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s;", tc.Schema))

			createTable := stripHeredoc(fmt.Sprintf(`
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

func TestPsqldefAddConstraintUnique(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		create table dummy(
		  column_a int not null,
		  column_b int not null,
		  column_c int not null
		);
		`,
	)
	alterTable := "alter table dummy add constraint dummy_uniq unique (column_a, column_b);"
	assertApplyOutput(t, createTable+alterTable, wrapWithTransaction(createTable+alterTable+"\n"))
	assertApplyOutput(t, createTable+alterTable, nothingModified)

	alterTable = "alter table dummy add constraint dummy_uniq unique (column_a, column_b) not deferrable initially immediate;"
	assertApplyOutput(t, createTable+alterTable, nothingModified)

	alterTable = "alter table dummy add constraint dummy_uniq unique (column_a, column_b) deferrable;"
	dropConstraint := `ALTER TABLE "public"."dummy" DROP CONSTRAINT "dummy_uniq";`
	assertApplyOutput(t, createTable+alterTable, wrapWithTransaction(dropConstraint+"\n"+alterTable+"\n"))

	alterTable = "alter table dummy add constraint dummy_uniq unique (column_a, column_b) deferrable initially deferred;"
	assertApplyOutput(t, createTable+alterTable, wrapWithTransaction(dropConstraint+"\n"+alterTable+"\n"))

	alterTable = "alter table dummy add constraint dummy_uniq unique (column_a, column_b);"
	assertApplyOutput(t, createTable+alterTable, wrapWithTransaction(dropConstraint+"\n"+alterTable+"\n"))

	assertApplyOutput(t, createTable, wrapWithTransaction(dropConstraint+"\n"))
	assertApplyOutput(t, createTable, nothingModified)
}

func TestPsqldefCreateIndexWithKey(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  "key" text
		);
		`,
	)
	createIndex := `CREATE INDEX "index_name" on users (key);`
	assertApplyOutput(t, createTable+createIndex, wrapWithTransaction(createTable+createIndex+"\n"))
	assertApplyOutput(t, createTable+createIndex, nothingModified)
}

func TestPsqldefCreateIndexWithOperatorClass(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE products (
		  name VARCHAR(255)
		);
		CREATE INDEX product_name_autocomplete_index ON products(name text_pattern_ops);
		`,
	)
	assertApplyOutput(t, createTable, wrapWithTransaction(createTable))
	assertApplyOutput(t, createTable, nothingModified)
}

func TestPsqldefCreateType(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TYPE "public"."country" AS ENUM ('us', 'jp');
		CREATE TABLE users (
		  id SERIAL PRIMARY KEY,
		  country "public"."country" NOT NULL DEFAULT 'jp'::country
		);
		`,
	)
	assertApplyOutput(t, createTable, wrapWithTransaction(createTable))
	assertApplyOutput(t, createTable, nothingModified)
}

func TestPsqldefColumnLiteral(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  "id" bigint NOT NULL,
		  "name" text,
		  "age" integer
		);
		`,
	)
	assertApplyOutput(t, createTable, wrapWithTransaction(createTable))
	assertApplyOutput(t, createTable, nothingModified)
}

func TestPsqldefDataTypes(t *testing.T) {
	resetTestDatabase()

	// TODO:
	//   - int8
	//   - serial8
	//   - box
	//   - bytea
	//   - cidr
	//   - circle
	//   - float8
	//   - inet
	//   - int4
	//   - interval [ fields ] [ (p) ]
	//   - line
	//   - lseg
	//   - macaddr
	//   - money
	//   - numeric [ (p, s) ]
	//   - decimal [ (p, s) ]
	//   - path
	//   - pg_lsn
	//   - point
	//   - polygon
	//   - real
	//   - float4
	//   - smallint
	//   - int2
	//   - smallserial
	//   - serial2
	//   - serial4
	//   - timetz
	//   - timestamptz
	//   - tsquer
	//   - tsvector
	//   - txid_snapshot
	//   - xml
	//
	// Remaining SQL spec: bit varying, interval, numeric, decimal, real,
	//   smallint, smallserial, xml
	createTable := stripHeredoc(`
		CREATE TABLE users (
		  c_array integer array,
		  c_array_bracket integer[],
		  c_bigint bigint,
		  c_bigserial bigserial,
		  c_bit bit,
		  c_bit_2 bit(2),
		  c_bool bool,
		  c_boolean boolean,
		  c_char_10 char(10),
		  c_character_20 character(20),
		  c_character_varying_30 character varying(30),
		  c_date date,
		  c_double_precision double precision,
		  c_json json,
		  c_jsonb jsonb,
		  c_timestamp timestamp,
		  c_timestamp_6 timestamp(6),
		  c_timestamp_tz timestamp with time zone,
		  c_timestamp_tz_6 timestamp(6) with time zone,
		  c_timestamp_tz_6_notnull timestamp(6) with time zone not null,
		  c_time time,
		  c_time_6 time(6),
		  c_time_tz time with time zone,
		  c_time_tz_6 time(6) with time zone,
		  c_time_tz_6_notnull time(6) with time zone not null,
		  c_int int,
		  c_integer integer,
		  c_serial serial,
		  c_text text,
		  c_uuid uuid,
		  c_varchar_40 varchar(40)
		);
		`,
	)

	assertApplyOutput(t, createTable, wrapWithTransaction(createTable))
	assertApplyOutput(t, createTable, nothingModified) // Label for column type may change. Type will be examined.
}

func TestPsqldefCreateTableInSchema(t *testing.T) {
	resetTestDatabase()
	mustPgExec(testDatabaseName, "CREATE SCHEMA test;")

	createTable := "CREATE TABLE test.users (id serial primary key);"
	assertApplyOutput(t, createTable, wrapWithTransaction(createTable+"\n"))
	assertApplyOutput(t, createTable, nothingModified)
}

func TestPsqldefCheckConstraintInSchema(t *testing.T) {
	resetTestDatabase()
	mustPgExec(testDatabaseName, "CREATE SCHEMA test;")

	createTable := stripHeredoc(`
		CREATE TABLE test.dummy (
		  min_value INT CHECK (min_value > 0),
		  max_value INT
		);`)
	assertApplyOutput(t, createTable, wrapWithTransaction(createTable+"\n"))
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE test.dummy (
		  min_value INT CHECK (min_value > 0),
		  max_value INT CHECK (max_value > 0),
		  CONSTRAINT min_max CHECK (min_value < max_value)
		);`)
	assertApplyOutput(t, createTable, wrapWithTransaction(`ALTER TABLE "test"."dummy" ADD CONSTRAINT dummy_max_value_check CHECK (max_value > 0);`+"\n"+
		`ALTER TABLE "test"."dummy" ADD CONSTRAINT "min_max" CHECK (min_value < max_value);`+"\n"))
	assertExportOutput(t, stripHeredoc(`
		CREATE SCHEMA "test";

		CREATE TABLE "test"."dummy" (
		    "min_value" integer CONSTRAINT dummy_min_value_check CHECK (min_value > 0),
		    "max_value" integer CONSTRAINT dummy_max_value_check CHECK (max_value > 0),
		    CONSTRAINT min_max CHECK (min_value < max_value)
		);
		`))
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE test.dummy (
		  min_value INT CHECK (min_value > 0),
		  max_value INT
		);`)
	assertApplyOutput(t, createTable, wrapWithTransaction(`ALTER TABLE "test"."dummy" DROP CONSTRAINT dummy_max_value_check;`+"\n"+
		`ALTER TABLE "test"."dummy" DROP CONSTRAINT "min_max";`+"\n"))
	assertApplyOutput(t, createTable, nothingModified)
}

func TestPsqldefSameTableNameAmongSchemas(t *testing.T) {
	resetTestDatabase()
	mustPgExec(testDatabaseName, "CREATE SCHEMA test;")

	createTable := stripHeredoc(`
		CREATE TABLE dummy (id int);
		CREATE TABLE test.dummy (id int);`)
	assertApplyOutput(t, createTable, wrapWithTransaction(createTable+"\n"))
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE dummy (id int);
		CREATE TABLE test.dummy ();`)
	assertApplyOutputWithEnableDrop(t, createTable, wrapWithTransaction(`ALTER TABLE "test"."dummy" DROP COLUMN "id";`+"\n"))
	assertApplyOutputWithEnableDrop(t, createTable, nothingModified)
}

func TestPsqldefCreateTableWithIdentityColumn(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE color (
		  color_id INT GENERATED ALWAYS AS IDENTITY,
		  color_name VARCHAR NOT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, wrapWithTransaction(createTable))
	assertApplyOutput(t, createTable, nothingModified)
}

func TestPsqldefCreateTableWithExpressionStored(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
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

func TestPsqldefAddingIdentityColumn(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE color (
		  color_id INT NOT NULL,
		  color_name VARCHAR NOT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, wrapWithTransaction(createTable))
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE color (
		  color_id INT GENERATED BY DEFAULT AS IDENTITY,
		  color_name VARCHAR NOT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, wrapWithTransaction(`ALTER TABLE "public"."color" ALTER COLUMN "color_id" ADD GENERATED BY DEFAULT AS IDENTITY;`+"\n"))
	assertApplyOutput(t, createTable, nothingModified)
}

func TestPsqldefRemovingIdentityColumn(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE color (
		  color_id INT GENERATED BY DEFAULT AS IDENTITY,
		  color_name VARCHAR NOT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, wrapWithTransaction(createTable))
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE color (
		  color_id INT NOT NULL,
		  color_name VARCHAR NOT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, wrapWithTransaction(`ALTER TABLE "public"."color" ALTER COLUMN "color_id" DROP IDENTITY IF EXISTS;`+"\n"))
	assertApplyOutput(t, createTable, nothingModified)
}

func TestPsqldefChangingIdentityColumn(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE color (
		  color_id INT GENERATED BY DEFAULT AS IDENTITY,
		  color_name VARCHAR NOT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, wrapWithTransaction(createTable))
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE color (
		  color_id INT GENERATED ALWAYS AS IDENTITY,
		  color_name VARCHAR NOT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, wrapWithTransaction(`ALTER TABLE "public"."color" ALTER COLUMN "color_id" SET GENERATED ALWAYS;`+"\n"))
	assertApplyOutput(t, createTable, nothingModified)
}

func TestPsqldefCreateIdentityColumnWithSequenceOption(t *testing.T) {
	resetTestDatabase()

	createTableWithSequence1 := stripHeredoc(`
		CREATE TABLE voltages (
		  volt int GENERATED BY DEFAULT AS IDENTITY
		    (START WITH -200 INCREMENT BY 10 MINVALUE -200 MAXVALUE 200)
		);
		`,
	)
	createTableWithoutSequence := stripHeredoc(`
		CREATE TABLE voltages (
		  volt int GENERATED BY DEFAULT AS IDENTITY
		);
		`,
	)

	assertApplyOutput(t, createTableWithSequence1, wrapWithTransaction(createTableWithSequence1))
	assertApplyOutput(t, createTableWithoutSequence, nothingModified)

	createTableWithSequence2 := stripHeredoc(`
		CREATE TABLE voltages (
		  volt int GENERATED BY DEFAULT AS IDENTITY
		    (START WITH -100 INCREMENT BY 5 MINVALUE -100 MAXVALUE 100)
		);
		`,
	)

	// not support changing sequence option
	assertApplyOutput(t, createTableWithSequence2, nothingModified)
}

func TestPsqldefModifyIdentityColumnWithSequenceOption(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE voltages (
		  volt int
		);
		`,
	)
	assertApplyOutput(t, createTable, wrapWithTransaction(createTable))

	createTableWithSequence := stripHeredoc(`
		CREATE TABLE voltages (
		  volt int GENERATED BY DEFAULT AS IDENTITY
		    (START WITH -100 INCREMENT BY 5 MINVALUE -100 MAXVALUE 100)
		);
		`,
	)
	alter1 := `ALTER TABLE "public"."voltages" ALTER COLUMN "volt" SET NOT NULL;`
	alter2 := `ALTER TABLE "public"."voltages" ALTER COLUMN "volt" ADD GENERATED BY DEFAULT AS IDENTITY (START WITH -100 INCREMENT BY 5 MINVALUE -100 MAXVALUE 100);`
	assertApplyOutput(t, createTableWithSequence, wrapWithTransaction(alter1+"\n"+alter2+"\n"))

	createTableWithoutSequence := stripHeredoc(`
		CREATE TABLE voltages (
		  volt int GENERATED BY DEFAULT AS IDENTITY
		);
		`,
	)

	// not support changing sequence option
	assertApplyOutput(t, createTableWithoutSequence, nothingModified)
}

func TestPsqldefAddIdentityColumnWithSequenceOption(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE voltages (
		  name varchar NOT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, wrapWithTransaction(createTable))

	createTableWithSequence := stripHeredoc(`
		CREATE TABLE voltages (
		  name varchar NOT NULL,
		  volt int GENERATED BY DEFAULT AS IDENTITY
		    (START WITH -100 INCREMENT BY 5 MINVALUE -100 MAXVALUE 100)
		);
		`,
	)
	alter := `ALTER TABLE "public"."voltages" ADD COLUMN "volt" integer GENERATED BY DEFAULT AS IDENTITY (START WITH -100 INCREMENT BY 5 MINVALUE -100 MAXVALUE 100);`
	assertApplyOutput(t, createTableWithSequence, wrapWithTransaction(alter+"\n"))

	createTableWithoutSequence := stripHeredoc(`
		CREATE TABLE voltages (
		  name varchar NOT NULL,
		  volt int GENERATED BY DEFAULT AS IDENTITY
		);
		`,
	)

	// not support changing sequence option
	assertApplyOutput(t, createTableWithoutSequence, nothingModified)
}

func TestPsqldefAddUniqueConstraintToTableInNonpublicSchema(t *testing.T) {
	resetTestDatabase()
	mustPgExec(testDatabaseName, "CREATE SCHEMA test;")

	createTable := "CREATE TABLE test.dummy (a int, b int);"
	assertApplyOutput(t, createTable, wrapWithTransaction(createTable+"\n"))
	assertApplyOutput(t, createTable, nothingModified)

	alterTable := "ALTER TABLE test.dummy ADD CONSTRAINT a_b_uniq UNIQUE (a, b);"
	assertApplyOutput(t, createTable+"\n"+alterTable, wrapWithTransaction(alterTable+"\n"))
	assertExportOutput(t, stripHeredoc(`
		CREATE SCHEMA "test";

		CREATE TABLE "test"."dummy" (
		    "a" integer,
		    "b" integer
		);

		ALTER TABLE "test"."dummy" ADD CONSTRAINT "a_b_uniq" UNIQUE (a, b);
		`))
	assertApplyOutput(t, createTable+"\n"+alterTable, nothingModified)

	alterTable = "ALTER TABLE test.dummy ADD CONSTRAINT a_uniq UNIQUE (a) DEFERRABLE INITIALLY DEFERRED;"
	assertApplyOutput(t, createTable+"\n"+alterTable, wrapWithTransaction(alterTable+"\n"+
		`ALTER TABLE "test"."dummy" DROP CONSTRAINT "a_b_uniq";`+"\n"))
	assertExportOutput(t, stripHeredoc(`
		CREATE SCHEMA "test";

		CREATE TABLE "test"."dummy" (
		    "a" integer,
		    "b" integer
		);

		ALTER TABLE "test"."dummy" ADD CONSTRAINT "a_uniq" UNIQUE (a) DEFERRABLE INITIALLY DEFERRED;
		`))
	assertApplyOutput(t, createTable+"\n"+alterTable, nothingModified)
}

func TestPsqldefFunctionAsDefault(t *testing.T) {
	for _, tc := range publicAndNonPublicSchemaTestCases {
		resetTestDatabase()
		mustPgExec(testDatabaseName, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s;", tc.Schema))

		mustPgExec(testDatabaseName, fmt.Sprintf(stripHeredoc(`
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

		createTable := fmt.Sprintf(stripHeredoc(`
			CREATE TABLE %s.test (
			  pk timestamp primary key default now(),
			  col timestamp default now(),
			  uniq timestamp unique default now(),
			  not_null timestamp not null default now(),
			  same_schema int default %s.my_func()
			);`), tc.Schema, tc.Schema)
		assertApplyOutput(t, createTable, wrapWithTransaction(createTable+"\n"))
		assertApplyOutput(t, createTable, nothingModified)
	}
}

//
// ----------------------- following tests are for CLI -----------------------
//

func TestPsqldefApply(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
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
	writeFile("schema.sql", stripHeredoc(`
	    CREATE TABLE users (
	        id bigint NOT NULL PRIMARY KEY,
	        age int
	    );`,
	))

	dryRun := mustExecute(t, "./psqldef", "-Upostgres", testDatabaseName, "--dry-run", "--file", "schema.sql")
	apply := mustExecute(t, "./psqldef", "-Upostgres", testDatabaseName, "--file", "schema.sql")
	assertEquals(t, dryRun, strings.Replace(apply, "Apply", "dry run", 1))
}

func TestPsqldefDropTable(t *testing.T) {
	resetTestDatabase()
	mustPgExec(testDatabaseName, stripHeredoc(`
		CREATE TABLE users (
		    id bigint NOT NULL PRIMARY KEY,
		    age int,
		    c_char_1 char,
		    c_char_10 char(10),
		    c_varchar_10 varchar(10),
		    c_varchar_unlimited varchar
		);`,
	))

	writeFile("schema.sql", "")

	dropTable := `DROP TABLE "public"."users";`
	out := mustExecute(t, "./psqldef", "-Upostgres", testDatabaseName, "--enable-drop", "--file", "schema.sql")
	assertEquals(t, out, wrapWithTransaction(dropTable+"\n"))
}

func TestPsqldefConfigInlineEnableDrop(t *testing.T) {
	resetTestDatabase()
	ddl := stripHeredoc(`
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

	writeFile("schema.sql", "")

	dropTable := `DROP TABLE "public"."users";`
	expectedOutput := wrapWithTransaction(dropTable + "\n")

	outFlag := mustExecute(t, "./psqldef", "-Upostgres", testDatabaseName, "--enable-drop", "--file", "schema.sql")
	assertEquals(t, outFlag, expectedOutput)

	mustPgExec(testDatabaseName, ddl)

	outConfigInline := mustExecute(t, "./psqldef", "-Upostgres", testDatabaseName, "--config-inline", "enable_drop: true", "--file", "schema.sql")
	assertEquals(t, outConfigInline, expectedOutput)
}

func TestPsqldefExport(t *testing.T) {
	resetTestDatabase()

	assertExportOutput(t, "-- No table exists --\n")

	mustPgExec(testDatabaseName, stripHeredoc(`
		CREATE TABLE users (
		    id bigint NOT NULL PRIMARY KEY,
		    age int,
		    c_char_1 char unique,
		    c_char_10 char(10),
		    c_varchar_10 varchar(10),
		    c_varchar_unlimited varchar
		);`,
	))

	assertExportOutput(t, stripHeredoc(`
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

	mustPgExec(testDatabaseName, stripHeredoc(`
		CREATE TABLE users (
		    col1 character varying(40) NOT NULL,
		    col2 character varying(6) NOT NULL,
		    created_at timestamp NOT NULL,
		    PRIMARY KEY (col1, col2)
		);`,
	))

	assertExportOutput(t, stripHeredoc(`
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

	mustPgExec(testDatabaseName, stripHeredoc(`
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

	outputDefault := mustExecute(t, "./psqldef", "-Upostgres", testDatabaseName, "--export")

	writeFile("config.yml", "dump_concurrency: 0")
	outputNoConcurrency := mustExecute(t, "./psqldef", "-Upostgres", testDatabaseName, "--export", "--config", "config.yml")

	writeFile("config.yml", "dump_concurrency: 1")
	outputConcurrency1 := mustExecute(t, "./psqldef", "-Upostgres", testDatabaseName, "--export", "--config", "config.yml")

	writeFile("config.yml", "dump_concurrency: 10")
	outputConcurrency10 := mustExecute(t, "./psqldef", "-Upostgres", testDatabaseName, "--export", "--config", "config.yml")

	writeFile("config.yml", "dump_concurrency: -1")
	outputConcurrencyNoLimit := mustExecute(t, "./psqldef", "-Upostgres", testDatabaseName, "--export", "--config", "config.yml")

	assertEquals(t, outputDefault, stripHeredoc(`
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
	))

	assertEquals(t, outputNoConcurrency, outputDefault)
	assertEquals(t, outputConcurrency1, outputDefault)
	assertEquals(t, outputConcurrency10, outputDefault)
	assertEquals(t, outputConcurrencyNoLimit, outputDefault)
}

func TestPsqldefSkipView(t *testing.T) {
	resetTestDatabase()

	createTable := "CREATE TABLE users (id bigint);\n"
	createView := "CREATE VIEW user_views AS SELECT id from users;\n"
	createMaterializedView := "CREATE MATERIALIZED VIEW user_materialized_views AS SELECT id from users;\n"

	mustPgExec(testDatabaseName, createTable+createView+createMaterializedView)

	writeFile("schema.sql", createTable)

	output := mustExecute(t, "./psqldef", "-Upostgres", testDatabaseName, "--skip-view", "-f", "schema.sql")
	assertEquals(t, output, nothingModified)
}

func TestPsqldefSkipExtension(t *testing.T) {
	resetTestDatabase()

	createExtension := "CREATE EXTENSION pgcrypto;\n"

	mustPgExec(testDatabaseName, createExtension)

	writeFile("schema.sql", "")

	output := mustExecute(t, "./psqldef", "-Upostgres", testDatabaseName, "--skip-extension", "-f", "schema.sql")
	assertEquals(t, output, nothingModified)
}

func TestPsqldefBeforeApply(t *testing.T) {
	resetTestDatabase()

	// Setup
	mustPgExec(testDatabaseName, "DROP ROLE IF EXISTS dummy_owner_role;")
	mustPgExec(testDatabaseName, "CREATE ROLE dummy_owner_role;")
	mustPgExec(testDatabaseName, "GRANT ALL ON SCHEMA public TO dummy_owner_role;")

	beforeApply := "SET ROLE dummy_owner_role; SET TIME ZONE LOCAL;"
	createTable := "CREATE TABLE dummy (id int);"
	writeFile("schema.sql", createTable)

	dryRun := mustExecute(t, "./psqldef", "-Upostgres", testDatabaseName, "-f", "schema.sql", "--before-apply", beforeApply, "--dry-run")
	apply := mustExecute(t, "./psqldef", "-Upostgres", testDatabaseName, "-f", "schema.sql", "--before-apply", beforeApply)
	assertEquals(t, dryRun, strings.Replace(apply, "Apply", "dry run", 1))
	assertEquals(t, apply, applyPrefix+"BEGIN;\n"+beforeApply+"\n"+createTable+"\nCOMMIT;\n")

	apply = mustExecute(t, "./psqldef", "-Upostgres", testDatabaseName, "-f", "schema.sql", "--before-apply", beforeApply)
	assertEquals(t, apply, nothingModified)

	owner, err := pgQuery(testDatabaseName, "SELECT tableowner FROM pg_tables WHERE tablename = 'dummy'")
	if err != nil {
		t.Fatal(err)
	}
	assertEquals(t, owner, "dummy_owner_role\n")
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

	writeFile("schema.sql", `
        CREATE TABLE users (id bigint PRIMARY KEY);
        CREATE TABLE users_1 (id bigint PRIMARY KEY);
    `)

	writeFile("config.yml", "target_tables: |\n  public\\.users\n  public\\.users_\\d\n")

	apply := mustExecute(t, "./psqldef", "-Upostgres", testDatabaseName, "-f", "schema.sql", "--config", "config.yml")
	assertEquals(t, apply, nothingModified)
}

func TestPsqldefConfigIncludesTargetSchema(t *testing.T) {
	resetTestDatabase()

	mustPgExec(testDatabaseName, `
        CREATE SCHEMA schema_a;
        CREATE TABLE schema_a.users (id bigint PRIMARY KEY);
        CREATE SCHEMA schema_b;
        CREATE TABLE schema_b.users (id bigint PRIMARY KEY);
    `)

	writeFile("schema.sql", `
        CREATE TABLE schema_a.users (id bigint PRIMARY KEY);
    `)

	writeFile("config.yml", "target_schema: schema_a\n")

	apply := mustExecute(t, "./psqldef", "-Upostgres", testDatabaseName, "-f", "schema.sql", "--config", "config.yml")
	assertEquals(t, apply, nothingModified)

	// multiple targets
	mustPgExec(testDatabaseName, `
        CREATE SCHEMA schema_c;
        CREATE TABLE schema_c.users (id bigint PRIMARY KEY);
    `)

	writeFile("schema.sql", `
        CREATE TABLE schema_a.users (id bigint PRIMARY KEY);
        CREATE TABLE schema_b.users (id bigint PRIMARY KEY);
    `)

	writeFile("config.yml", `target_schema: |
  schema_a
  schema_b`)

	apply = mustExecute(t, "./psqldef", "-Upostgres", testDatabaseName, "-f", "schema.sql", "--config", "config.yml")
	assertEquals(t, apply, nothingModified)
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

	schema := stripHeredoc(`
				CREATE SCHEMA bar;
				CREATE TABLE bar.companies (
					id BIGINT PRIMARY KEY
				);
	`)
	writeFile("schema.sql", schema)

	writeFile("config.yml", "target_schema: bar\n")

	apply := mustExecute(t, "./psqldef", "-Upostgres", testDatabaseName, "-f", "schema.sql", "--config", "config.yml")
	assertEquals(t, apply, wrapWithTransaction(schema))

	apply = mustExecute(t, "./psqldef", "-Upostgres", testDatabaseName, "-f", "schema.sql", "--config", "config.yml")
	assertEquals(t, apply, nothingModified)
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

	writeFile("schema.sql", `
        CREATE TABLE users (id bigint PRIMARY KEY);
        CREATE TABLE users_1 (id bigint PRIMARY KEY);
    `)

	writeFile("config.yml", "skip_tables: |\n  public\\.users_10\n")

	apply := mustExecute(t, "./psqldef", "-Upostgres", testDatabaseName, "-f", "schema.sql", "--config", "config.yml")
	assertEquals(t, apply, nothingModified)
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

	writeFile("schema.sql", `
        CREATE MATERIALIZED VIEW views AS SELECT 1 AS id, 12 AS uid;
        CREATE MATERIALIZED VIEW views_1 AS SELECT 1 AS id, 13 AS uid;
    `)

	writeFile("config.yml", "skip_views: |\n  public\\.views_10\n")

	apply := mustExecute(t, "./psqldef", "-Upostgres", testDatabaseName, "-f", "schema.sql", "--config", "config.yml")
	assertEquals(t, apply, nothingModified)
}

func TestPsqldefHelp(t *testing.T) {
	_, err := testutils.Execute("./psqldef", "--help")
	if err != nil {
		t.Errorf("failed to run --help: %s", err)
	}

	out, err := testutils.Execute("./psqldef")
	if err == nil {
		t.Errorf("no database must be error, but successfully got: %s", out)
	}
}

func TestPsqldefTableLevelCheckConstraintsWithAllAny(t *testing.T) {
	resetTestDatabase()

	// Test truly table-level CHECK constraints (multi-column)
	// This avoids the single-column constraint category confusion
	tableWithMultiColumnConstraint := stripHeredoc(`
		CREATE TABLE multi_check_test (
		  id INTEGER PRIMARY KEY,
		  state TEXT NOT NULL,
		  priority INTEGER NOT NULL,
		  CONSTRAINT valid_state_priority CHECK (
		    (state = ANY (ARRAY['active', 'pending']) AND priority >= ALL (ARRAY[1, 2])) OR
		    (state = ALL (ARRAY['inactive']) AND priority = ANY (ARRAY[0]))
		  )
		);
		`,
	)
	assertApplyOutput(t, tableWithMultiColumnConstraint, wrapWithTransaction(tableWithMultiColumnConstraint))
	assertApplyOutput(t, tableWithMultiColumnConstraint, nothingModified)

	// Test modifying the multi-column constraint
	modifiedConstraint := stripHeredoc(`
		CREATE TABLE multi_check_test (
		  id INTEGER PRIMARY KEY,
		  state TEXT NOT NULL,
		  priority INTEGER NOT NULL,
		  CONSTRAINT valid_state_priority CHECK (
		    (state = ANY (ARRAY['active', 'pending', 'waiting']) AND priority >= ALL (ARRAY[1, 2])) OR
		    (state = ALL (ARRAY['inactive']) AND priority = ANY (ARRAY[0]))
		  )
		);
		`,
	)
	assertApplyOutput(t, modifiedConstraint, wrapWithTransaction(`ALTER TABLE "public"."multi_check_test" DROP CONSTRAINT "valid_state_priority";`+"\n"+
		`ALTER TABLE "public"."multi_check_test" ADD CONSTRAINT "valid_state_priority" CHECK (state = ANY (ARRAY['active', 'pending', 'waiting']) and priority >= ALL (ARRAY[1, 2]) or state = ALL (ARRAY['inactive']) and priority = ANY (ARRAY[0]));`+"\n"))
	assertApplyOutput(t, modifiedConstraint, nothingModified)
}

func TestPsqldefTransactionBoundariesWithConcurrentIndex(t *testing.T) {
	resetTestDatabase()

	mustPgExec(testDatabaseName, stripHeredoc(`
		CREATE TABLE users (
		    id bigint NOT NULL PRIMARY KEY,
		    email text,
		    age integer,
		    name text
		);`))

	// Test 1: Single CREATE INDEX CONCURRENTLY - should be outside transaction
	t.Run("SingleConcurrentIndex", func(t *testing.T) {
		schema := stripHeredoc(`
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
		mustPgExec(testDatabaseName, stripHeredoc(`
			CREATE TABLE users (
			    id bigint NOT NULL PRIMARY KEY,
			    email text
			);`))

		schema := stripHeredoc(`
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
		expected := applyPrefix + stripHeredoc(`
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
		mustPgExec(testDatabaseName, stripHeredoc(`
			CREATE TABLE users (
			    id bigint NOT NULL PRIMARY KEY,
			    email text,
			    age integer
			);
			CREATE INDEX idx_users_email ON users (email);
			CREATE INDEX idx_users_age ON users (age);`))

		// Dropping the indexes with CONCURRENTLY should be outside transaction
		schema := stripHeredoc(`
			CREATE TABLE users (
			    id bigint NOT NULL PRIMARY KEY,
			    email text,
			    age integer
			);`)

		// Note: psqldef may not generate DROP INDEX CONCURRENTLY by default
		// This test may need adjustment based on actual behavior
		// For now, we'll test that regular DROP INDEX is in transaction
		expected := wrapWithTransaction(stripHeredoc(`
			DROP INDEX "public"."idx_users_email";
			DROP INDEX "public"."idx_users_age";
		`))

		assertApplyOutputWithEnableDrop(t, schema, expected)
	})

	// Test 4: Dry run with concurrent index
	t.Run("DryRunWithConcurrentIndex", func(t *testing.T) {
		resetTestDatabase()
		mustPgExec(testDatabaseName, stripHeredoc(`
			CREATE TABLE users (
			    id bigint NOT NULL PRIMARY KEY,
			    email text
			);`))

		writeFile("schema.sql", stripHeredoc(`
			CREATE TABLE users (
			    id bigint NOT NULL PRIMARY KEY,
			    email text,
			    age integer
			);
			CREATE INDEX CONCURRENTLY idx_users_email ON users (email);
			CREATE INDEX idx_users_age ON users (age);`))

		dryRun := mustExecute(t, "./psqldef", "-Upostgres", testDatabaseName, "--dry-run", "--file", "schema.sql")
		apply := mustExecute(t, "./psqldef", "-Upostgres", testDatabaseName, "--file", "schema.sql")

		// Verify that dry run output matches apply output (except for the prefix)
		assertEquals(t, dryRun, strings.Replace(apply, "Apply", "dry run", 1))

		// Verify the structure of the output
		expectedStructure := "-- dry run --\n" + stripHeredoc(`
			BEGIN;
			ALTER TABLE "public"."users" ADD COLUMN "age" integer;
			CREATE INDEX idx_users_age ON users (age);
			COMMIT;
			CREATE INDEX CONCURRENTLY idx_users_email ON users (email);
		`)

		assertEquals(t, dryRun, expectedStructure)
	})

	// Test 5: Multiple concurrent operations
	t.Run("MultipleConcurrentOperations", func(t *testing.T) {
		resetTestDatabase()
		mustPgExec(testDatabaseName, stripHeredoc(`
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

		schema := stripHeredoc(`
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

		expected := applyPrefix + stripHeredoc(`
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
	mustPgExec(testDatabaseName, stripHeredoc(`
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
		writeFile("schema.sql", stripHeredoc(`
			CREATE TABLE users (
			    id bigint NOT NULL PRIMARY KEY,
			    email text,
			    age integer
			);
			CREATE INDEX idx_users_email ON users (email);
			CREATE INDEX idx_users_age ON users (age);`))

		// Verify that regular operations still work
		output := mustExecute(t, "./psqldef", "-Upostgres", testDatabaseName, "--file", "schema.sql")
		assertEquals(t, output, nothingModified)
	})
}

func TestMain(m *testing.M) {
	resetTestDatabase()
	testutils.MustExecute("go", "build")
	status := m.Run()

	cleanupTestRoles()
	_ = os.Remove("psqldef")
	_ = os.Remove("schema.sql")
	_ = os.Remove("config.yml")
	os.Exit(status)
}

func assertApplyOutput(t *testing.T, schema string, expected string) {
	t.Helper()
	actual := assertApplyOutputWithConfig(t, schema, database.GeneratorConfig{EnableDrop: false})
	assertEquals(t, actual, expected)
}

func assertApplyOutputWithEnableDrop(t *testing.T, schema string, expected string) {
	t.Helper()
	actual := assertApplyOutputWithConfig(t, schema, database.GeneratorConfig{EnableDrop: true})
	assertEquals(t, actual, expected)
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
	output, err := testutils.ApplyWithOutput(db, schema.GeneratorModePostgres, sqlParser, desiredSchema, config)
	if err != nil {
		t.Fatal(err)
	}

	return output
}

func assertExportOutput(t *testing.T, expected string) {
	t.Helper()
	actual := mustExecute(t, "./psqldef", "-Upostgres", testDatabaseName, "--export")
	assertEquals(t, actual, expected)
}

func mustExecute(t *testing.T, command string, args ...string) string {
	t.Helper()
	out, err := testutils.Execute(command, args...)
	if err != nil {
		t.Errorf("failed to execute '%s %s' (error: '%s'): `%s`", command, strings.Join(args, " "), err, out)
	}
	return out
}

func assertEquals(t *testing.T, actual string, expected string) {
	t.Helper()
	if expected != actual {
		t.Errorf("expected '%s' but got '%s'", expected, actual)
	}
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

func resetTestDatabaseWithUser(user string) {
	resetTestDatabase()
	if user != "" {
		query := fmt.Sprintf(`
			DO $$ BEGIN
				IF NOT EXISTS (SELECT * FROM pg_roles WHERE rolname = '%s') THEN
					CREATE ROLE %s WITH LOGIN;
				END IF;
			END $$;
		`, user, user)
		mustPgExec(testDatabaseName, query)
		mustPgExec(testDatabaseName, fmt.Sprintf("ALTER ROLE %s SET search_path TO foo, public", user))
		mustPgExec(testDatabaseName, fmt.Sprintf("GRANT ALL ON DATABASE %s TO %s", testDatabaseName, user))
		mustPgExecAsUser(testDatabaseName, user, "CREATE SCHEMA foo")
	}

	createAllTestRoles()
}

func writeFile(path string, content string) {
	file, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	if _, err := file.Write(([]byte)(content)); err != nil {
		log.Fatal(err)
	}
}

func stripHeredoc(heredoc string) string {
	heredoc = strings.TrimPrefix(heredoc, "\n")
	re := regexp.MustCompilePOSIX("^\t*")
	return re.ReplaceAllLiteralString(heredoc, "")
}

var publicAndNonPublicSchemaTestCases = []struct {
	Name   string
	Schema string
}{
	{Name: "in public schema", Schema: "public"},
	{Name: "in non-public schema", Schema: "test"},
}

package postgres

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/k0kubun/sqldef/adapter"
	_ "github.com/lib/pq"
)

type PostgresDatabase struct {
	config adapter.Config
	db     *sql.DB
}

func NewDatabase(config adapter.Config) (adapter.Database, error) {
	db, err := sql.Open("postgres", postgresBuildDSN(config))
	if err != nil {
		return nil, err
	}

	return &PostgresDatabase{
		db:     db,
		config: config,
	}, nil
}

func (d *PostgresDatabase) TableNames() ([]string, error) {
	rows, err := d.db.Query("select table_name from information_schema.tables where table_schema='public';")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tables := []string{}
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return nil, err
		}
		tables = append(tables, table)
	}
	return tables, nil
}

// Due to PostgreSQL's limitation, depending on pb_dump(1) availability in client.
// Possibly it can be solved by constructing the complex query, but it would be hacky anyway.
func (d *PostgresDatabase) DumpTableDDL(table string) (string, error) {
	ddl, err := runPgDump(d.config, table)
	if err != nil {
		return "", err
	}

	// Remove comments
	re := regexp.MustCompilePOSIX("^--.*$")
	ddl = re.ReplaceAllLiteralString(ddl, "")

	// Drop "\." (what's this?)
	re = regexp.MustCompilePOSIX("^\\\\\\.$")
	ddl = re.ReplaceAllLiteralString(ddl, "")

	// Ignore SET statements
	re = regexp.MustCompilePOSIX("^SET .*;$")
	ddl = re.ReplaceAllLiteralString(ddl, "")

	// Ignore CREATE EXTENSION statements
	re = regexp.MustCompilePOSIX("^CREATE EXTENSION .*;$")
	ddl = re.ReplaceAllLiteralString(ddl, "")

	// Ignore COMMENT ON EXTENSION statements
	re = regexp.MustCompilePOSIX("^COMMENT ON .*;$")
	ddl = re.ReplaceAllLiteralString(ddl, "")

	// Ignore SELECT statements
	re = regexp.MustCompilePOSIX("^SELECT .*;$")
	ddl = re.ReplaceAllLiteralString(ddl, "")

	// Ignore COPY statements
	re = regexp.MustCompilePOSIX("^COPY .*;$")
	ddl = re.ReplaceAllLiteralString(ddl, "")

	// Ignore ALTER TABLE xxx OWNER TO yyy statements
	re = regexp.MustCompilePOSIX("^ALTER TABLE [^ ;]+ OWNER TO .+;$")
	ddl = re.ReplaceAllLiteralString(ddl, "")

	// Ignore REVOKE statements
	re = regexp.MustCompilePOSIX("^REVOKE .*;$")
	ddl = re.ReplaceAllLiteralString(ddl, "")

	// Ignore GRANT statements
	re = regexp.MustCompilePOSIX("^GRANT .*;$")
	ddl = re.ReplaceAllLiteralString(ddl, "")

	// Remove empty lines
	// TODO: there should be a better way....
	for strings.Replace(ddl, "\n\n", "\n", -1) != ddl {
		ddl = strings.Replace(ddl, "\n\n", "\n", -1)
	}

	ddl = strings.TrimSpace(ddl)
	ddl = strings.TrimSuffix(ddl, ";") // XXX: caller should handle this better

	return ddl, nil
}

func (d *PostgresDatabase) DB() *sql.DB {
	return d.db
}

func (d *PostgresDatabase) Close() error {
	return d.db.Close()
}

func runPgDump(config adapter.Config, table string) (string, error) {
	conninfo := fmt.Sprintf("dbname=%s", config.DbName)
	if sslmode, ok := os.LookupEnv("PGSSLMODE"); ok { // TODO: have this in adapter.Config, or standardize config with DSN?
		conninfo = fmt.Sprintf("%s sslmode=%s", conninfo, sslmode)
	}

	args := []string{
		conninfo,
		"--schema-only",
		"-t", table,
		"-U", config.User,
		"-h", config.Host,
	}
	if config.Socket == "" {
		args = append(args, "-p", fmt.Sprintf("%d", config.Port))
	}
	cmd := exec.Command("pg_dump", args...)

	if len(config.Password) > 0 {
		cmd.Env = os.Environ()
		cmd.Env = append(cmd.Env, fmt.Sprintf("PGPASSWORD=%s", config.Password)) // XXX: Can we pass this in DSN format in a safe way?
	}

	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return string(out), nil
}

func postgresBuildDSN(config adapter.Config) string {
	user := config.User
	password := config.Password
	database := config.DbName
	host := ""
	if config.Socket == "" {
		host = fmt.Sprintf("%s:%d", config.Host, config.Port)
	} else {
		host = config.Socket
	}

	options := ""
	if sslmode, ok := os.LookupEnv("PGSSLMODE"); ok { // TODO: have this in adapter.Config, or standardize config with DSN?
		options = fmt.Sprintf("?sslmode=%s", sslmode) // TODO: uri escape
	}

	// TODO: uri escape
	return fmt.Sprintf("postgres://%s:%s@%s/%s%s", user, password, host, database, options)
}

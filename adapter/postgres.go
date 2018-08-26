package adapter

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	_ "github.com/lib/pq"
)

func postgresBuildDSN(config Config) string {
	user := config.User
	password := config.Password
	host := fmt.Sprintf("%s:%d", config.Host, config.Port)
	database := config.DbName

	// TODO: uri escape
	return fmt.Sprintf("postgres://%s:%s@%s/%s", user, password, host, database)
}

func (d *Database) postgresTableNames() ([]string, error) {
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
func (d *Database) postgresDumpTableDDL(table string) (string, error) {
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

	// Ignore ALTER TABLE xxx ADD CONSTRAINT TO yyy statements
	// TODO: Why not parse this? Should this be considered as primary key?
	re = regexp.MustCompilePOSIX("^ALTER TABLE ONLY [^ ;]+\n +ADD CONSTRAINT [^ ;]+ PRIMARY KEY \\([^)]+\\);$")
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

func runPgDump(config Config, table string) (string, error) {
	cmd := exec.Command(
		"pg_dump", config.DbName, "-t", table,
		"-U", config.User, "-h", config.Host, "-p", fmt.Sprintf("%d", config.Port),
	)
	if len(config.Password) > 0 {
		cmd.Env = os.Environ()
		cmd.Env = append(cmd.Env, fmt.Sprintf("PGPASSWORD=%s", config.Password))
	}

	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return string(out), nil
}

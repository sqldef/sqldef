package driver

import (
	"fmt"
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

func (d *Database) postgresDumpTableDDL(table string) (string, error) {
	return "", nil // TODO
}

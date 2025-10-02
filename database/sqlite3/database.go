package sqlite3

import (
	"database/sql"
	"strings"

	"github.com/sqldef/sqldef/v3/database"
	_ "modernc.org/sqlite"
)

type Sqlite3Database struct {
	config database.Config
	db     *sql.DB
}

func NewDatabase(config database.Config) (database.Database, error) {
	db, err := sql.Open("sqlite", config.DbName)
	if err != nil {
		return nil, err
	}

	return &Sqlite3Database{
		db:     db,
		config: config,
	}, nil
}

func (d *Sqlite3Database) ExportDDLs() (string, error) {
	var ddls []string

	tableNames, err := d.tableNames()
	if err != nil {
		return "", err
	}
	for _, tableName := range tableNames {
		ddl, err := d.exportTableDDL(tableName)
		if err != nil {
			return "", err
		}

		ddls = append(ddls, ddl)
	}

	viewDDLs, err := d.views()
	if err != nil {
		return "", err
	}
	ddls = append(ddls, viewDDLs...)

	indexDDLs, err := d.indexes()
	if err != nil {
		return "", err
	}
	ddls = append(ddls, indexDDLs...)

	triggerDDLs, err := d.triggers()
	if err != nil {
		return "", err
	}
	ddls = append(ddls, triggerDDLs...)

	return strings.Join(ddls, "\n\n"), nil
}

func (d *Sqlite3Database) tableNames() ([]string, error) {
	rows, err := d.db.Query(
		`select tbl_name from sqlite_master where type = 'table' and tbl_name not like 'sqlite_%'`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tables := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tables = append(tables, name)
	}
	return tables, nil
}

func (d *Sqlite3Database) exportTableDDL(table string) (string, error) {
	const query = `select sql from sqlite_master where tbl_name = ? and type = 'table'`
	var sql string
	err := d.db.QueryRow(query, table).Scan(&sql)
	return sql + ";", err
}

func (d *Sqlite3Database) views() ([]string, error) {
	var ddls []string
	const query = "select sql from sqlite_master where type = 'view';"
	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var sql string
		if err = rows.Scan(&sql); err != nil {
			return nil, err
		}
		ddls = append(ddls, sql+";")
	}

	return ddls, nil
}

func (d *Sqlite3Database) indexes() ([]string, error) {
	var ddls []string
	// Exclude automatically generated indexes for unique constraint
	const query = "select sql from sqlite_master where type = 'index' and sql is not null;"
	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var sql string
		if err = rows.Scan(&sql); err != nil {
			return nil, err
		}
		ddls = append(ddls, sql+";")
	}

	return ddls, nil
}

func (d *Sqlite3Database) triggers() ([]string, error) {
	var ddls []string
	const query = "select sql from sqlite_master where type = 'trigger' and sql is not null;"
	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var sql string
		if err = rows.Scan(&sql); err != nil {
			return nil, err
		}
		ddls = append(ddls, sql+";")
	}

	return ddls, nil
}

func (d *Sqlite3Database) DB() *sql.DB {
	return d.db
}

func (d *Sqlite3Database) Close() error {
	return d.db.Close()
}

func (d *Sqlite3Database) GetDefaultSchema() string {
	return ""
}

func (d *Sqlite3Database) SetGeneratorConfig(config database.GeneratorConfig) {
	// Not implemented for sqlite3 - privileges not supported
}

func (d *Sqlite3Database) GetTransactionQueries() database.TransactionQueries {
	return database.TransactionQueries{
		Begin:    "BEGIN",
		Commit:   "COMMIT",
		Rollback: "ROLLBACK",
	}
}

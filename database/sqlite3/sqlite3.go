package sqlite3

import (
	"database/sql"

	"github.com/k0kubun/sqldef/database"
	_ "github.com/mattn/go-sqlite3"
)

type Sqlite3Database struct {
	config database.Config
	db     *sql.DB
}

func NewDatabase(config database.Config) (database.Database, error) {
	db, err := sql.Open("sqlite3", config.DbName)
	if err != nil {
		return nil, err
	}

	return &Sqlite3Database{
		db:     db,
		config: config,
	}, nil
}

func (d *Sqlite3Database) TableNames() ([]string, error) {
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

func (d *Sqlite3Database) DumpTableDDL(table string) (string, error) {
	const query = `select sql from sqlite_master where tbl_name = ?`
	var sql string
	err := d.db.QueryRow(query, table).Scan(&sql)
	return sql + ";", err
}

func (d *Sqlite3Database) Views() ([]string, error) {
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

func (d *Sqlite3Database) Triggers() ([]string, error) {
	return nil, nil
}

func (d *Sqlite3Database) Types() ([]string, error) {
	return nil, nil
}

func (d *Sqlite3Database) DB() *sql.DB {
	return d.db
}

func (d *Sqlite3Database) Close() error {
	return d.db.Close()
}

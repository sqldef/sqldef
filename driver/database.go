// This package has database driver layer. Never deal with DDL construction.
package driver

import (
	"database/sql"
	"fmt"
	"strings"
)

type Config struct {
	DbType string // TODO: convert to enum?
	DbName string
}

// Abstraction layer for multiple kinds of databases
type Database struct {
	config Config
	db     *sql.DB
}

func NewDatabase(config Config) (*Database, error) {
	var dsn string

	switch config.DbType {
	case "mysql":
		dsn = mysqlBuildDSN(config)
	case "postgres":
		dsn = postgresBuildDSN(config)
	default:
		return nil, fmt.Errorf("database type must be 'mysql' or 'postgres'")
	}

	db, err := sql.Open(config.DbType, dsn)
	if err != nil {
		return nil, err
	}

	return &Database{
		db:     db,
		config: config,
	}, nil
}

func (d *Database) Close() error {
	return d.db.Close()
}

func (d *Database) tableNames() ([]string, error) {
	switch d.config.DbType {
	case "mysql":
		return d.mysqlTableNames()
	case "postgres":
		return d.postgresTableNames()
	default:
		panic("unexpected DbType: " + d.config.DbType)
	}
}

func (d *Database) dumpTableDDL(table string) (string, error) {
	switch d.config.DbType {
	case "mysql":
		return d.mysqlDumpTableDDL(table)
	case "postgres":
		return d.postgresDumpTableDDL(table)
	default:
		panic("unexpected DbType: " + d.config.DbType)
	}
}

func (d *Database) DumpDDLs() (string, error) {
	ddls := []string{}
	tableNames, err := d.tableNames()
	if err != nil {
		return "", err
	}

	for _, tableName := range tableNames {
		ddl, err := d.dumpTableDDL(tableName)
		if err != nil {
			return "", err
		}

		ddls = append(ddls, ddl)
	}
	return strings.Join(ddls, ";\n\n"), nil
}

func (d *Database) RunDDLs(ddls []string) error {
	transaction, err := d.db.Begin()
	if err != nil {
		return err
	}
	for _, ddl := range ddls {
		fmt.Printf("Run: '%s'\n", ddl)
		if _, err := transaction.Exec(ddl); err != nil {
			transaction.Rollback()
			return err
		}
	}
	transaction.Commit()
	return nil
}

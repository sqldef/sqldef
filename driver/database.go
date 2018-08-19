// This package has database driver layer. Never deal with DDL construction.
package driver

import (
	"database/sql"
	"fmt"
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

func (d *Database) TableNames() ([]string, error) {
	switch d.config.DbType {
	case "mysql":
		return d.mysqlTableNames()
	case "postgres":
		return d.postgresTableNames()
	default:
		panic("unexpected DbType: " + d.config.DbType)
	}
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

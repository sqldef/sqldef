// This package has database driver layer. Never deal with DDL construction.
package driver

import (
	"database/sql"
	"fmt"
)

type Config struct {
	DbType string
	DbName string
}

// Abstraction layer for multiple kinds of databases
type Database struct {
	config Config
	db     *sql.DB
}

func NewDatabase(config Config) (*Database, error) {
	var db *sql.DB
	var err error

	switch config.DbType {
	case "mysql":
		if db, err = sql.Open("mysql", mysqlBuildDSN(config)); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("database type must be 'mysql' or 'postgresql'")
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

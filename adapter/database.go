// This package has database adapter layer. Never deal with DDL construction.
package adapter

import (
	"database/sql"
	"fmt"
	"strings"
)

type DatabaseType int

const (
	DatabaseTypeMysql = DatabaseType(iota)
	DatabaseTypePostgres
)

type Config struct {
	DbType   DatabaseType
	DbName   string
	User     string
	Password string
	Host     string
	Port     int
}

func (c *Config) databaseTypeName() string {
	switch c.DbType {
	case DatabaseTypeMysql:
		return "mysql"
	case DatabaseTypePostgres:
		return "postgres"
	default:
		panic(fmt.Sprintf("unexpected DbType %d is used in databaseType", c.DbType))
	}
}

// Abstraction layer for multiple kinds of databases
type Database struct {
	config Config
	db     *sql.DB
}

func NewDatabase(config Config) (*Database, error) {
	var dsn string

	switch config.DbType {
	case DatabaseTypeMysql:
		dsn = mysqlBuildDSN(config)
	case DatabaseTypePostgres:
		dsn = postgresBuildDSN(config)
	default:
		return nil, fmt.Errorf("unexpected database type %d in NewDatabase", config.DbType)
	}

	db, err := sql.Open(config.databaseTypeName(), dsn)
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
	case DatabaseTypeMysql:
		return d.mysqlTableNames()
	case DatabaseTypePostgres:
		return d.postgresTableNames()
	default:
		return nil, fmt.Errorf("unexpected DbType %d in tableNames", d.config.DbType)
	}
}

func (d *Database) dumpTableDDL(table string) (string, error) {
	switch d.config.DbType {
	case DatabaseTypeMysql:
		return d.mysqlDumpTableDDL(table)
	case DatabaseTypePostgres:
		return d.postgresDumpTableDDL(table)
	default:
		return "", fmt.Errorf("unexpected DbType %d in dumpTableDDL", d.config.DbType)
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
		fmt.Printf("Run: '%s;'\n", ddl)
		if _, err := transaction.Exec(ddl); err != nil {
			transaction.Rollback()
			return err
		}
	}
	transaction.Commit()
	return nil
}

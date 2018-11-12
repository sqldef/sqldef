// This package has database adapter layer. Never deal with DDL construction.
package adapter

import (
	"database/sql"
	"fmt"
	"strings"
)

type Config struct {
	DbName   string
	User     string
	Password string
	Host     string
	Port     int
	Socket   string
}

// Abstraction layer for multiple kinds of databases
type Database interface {
	TableNames() ([]string, error)
	DumpTableDDL(table string) (string, error)
	DB() *sql.DB
	Close() error
}

func DumpDDLs(d Database) (string, error) {
	ddls := []string{}
	tableNames, err := d.TableNames()
	if err != nil {
		return "", err
	}

	for _, tableName := range tableNames {
		ddl, err := d.DumpTableDDL(tableName)
		if err != nil {
			return "", err
		}

		ddls = append(ddls, ddl)
	}
	return strings.Join(ddls, ";\n\n"), nil
}

func RunDDLs(d Database, ddls []string) error {
	transaction, err := d.DB().Begin()
	if err != nil {
		return err
	}
	fmt.Println("-- Apply --")
	for _, ddl := range ddls {
		fmt.Printf("%s;\n", ddl)
		if _, err := transaction.Exec(ddl); err != nil {
			transaction.Rollback()
			return err
		}
	}
	transaction.Commit()
	return nil
}

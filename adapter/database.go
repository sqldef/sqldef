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

	// Only MySQL
	MySQLEnableCleartextPlugin bool
}

// Abstraction layer for multiple kinds of databases
type Database interface {
	TableNames() ([]string, error)
	DumpTableDDL(table string) (string, error)
	Views() ([]string, error)
	Triggers() ([]string, error)
	Types() ([]string, error)
	DB() *sql.DB
	Close() error
}

// TODO: This should probably be part of the Database interface
func DumpDDLs(d Database) (string, error) {
	ddls := []string{}

	typeDDLs, err := d.Types()
	if err != nil {
		return "", err
	}
	ddls = append(ddls, typeDDLs...)

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

	viewDDLs, err := d.Views()
	if err != nil {
		return "", err
	}
	ddls = append(ddls, viewDDLs...)

	triggerDDLs, err := d.Triggers()
	if err != nil {
		return "", err
	}
	ddls = append(ddls, triggerDDLs...)

	return strings.Join(ddls, "\n\n"), nil
}

func RunDDLs(d Database, ddls []string, skipDrop bool, beforeApply string) error {
	transaction, err := d.DB().Begin()
	if err != nil {
		return err
	}
	fmt.Println("-- Apply --")
	if len(beforeApply) > 0 {
		fmt.Println(beforeApply)
		if _, err := transaction.Exec(beforeApply); err != nil {
			transaction.Rollback()
			return err
		}
	}
	for _, ddl := range ddls {
		if skipDrop && strings.Contains(ddl, "DROP") {
			fmt.Printf("-- Skipped: %s;\n", ddl)
			continue
		}
		fmt.Printf("%s;\n", ddl)
		if _, err := transaction.Exec(ddl); err != nil {
			transaction.Rollback()
			return err
		}
	}
	transaction.Commit()
	return nil
}

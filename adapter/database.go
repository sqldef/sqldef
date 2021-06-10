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
	Views() ([]string, error)
	Constraints(table string) ([]*ColumnConstraints, error)
	DB() *sql.DB
	Close() error
}

type ColumnConstraints struct {
	Name        string
	Constraints []*Constraint
}

type Constraint struct {
	Name string
	Type ConstraintType
}

type ConstraintType int

const (
	ConstraintTypePK ConstraintType = iota
	ConstraintTypeDF
)

func DumpDDLs(d Database) (string, map[string][]*ColumnConstraints, error) {
	ddls := []string{}
	tableNames, err := d.TableNames()
	if err != nil {
		return "", nil, err
	}

	constraints := make(map[string][]*ColumnConstraints)
	for _, tableName := range tableNames {
		ddl, err := d.DumpTableDDL(tableName)
		if err != nil {
			return "", nil, err
		}

		columnConstraints, err := d.Constraints(tableName)
		if err != nil {
			return "", nil, err
		}
		constraints[tableName] = columnConstraints

		ddls = append(ddls, ddl)
	}

	viewDDLs, err := d.Views()
	if err != nil {
		return "", nil, err
	}
	ddls = append(ddls, viewDDLs...)

	return strings.Join(ddls, ";\n\n"), constraints, nil
}

func RunDDLs(d Database, ddls []string, skipDrop bool) error {
	transaction, err := d.DB().Begin()
	if err != nil {
		return err
	}
	fmt.Println("-- Apply --")
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

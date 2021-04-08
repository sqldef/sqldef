// This package has database adapter layer. Never deal with DDL construction.
package adapter

import (
	"context"
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
	TableNames(ctx context.Context) ([]string, error)
	DumpTableDDL(ctx context.Context, table string) (string, error)
	Views(ctx context.Context) ([]string, error)
	DB() *sql.DB
	Close() error
}

func DumpDDLs(ctx context.Context, d Database) (string, error) {
	ddls := []string{}
	tableNames, err := d.TableNames(ctx)
	if err != nil {
		return "", err
	}

	for _, tableName := range tableNames {
		ddl, err := d.DumpTableDDL(ctx, tableName)
		if err != nil {
			return "", err
		}

		ddls = append(ddls, ddl)
	}

	viewDDLs, err := d.Views(ctx)
	if err != nil {
		return "", err
	}
	ddls = append(ddls, viewDDLs...)

	return strings.Join(ddls, ";\n\n"), nil
}

func RunDDLs(ctx context.Context, d Database, ddls []string, skipDrop bool) error {
	transaction, err := d.DB().BeginTx(ctx, nil)
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
		if _, err := transaction.ExecContext(ctx, ddl); err != nil {
			transaction.Rollback()
			return err
		}
	}
	transaction.Commit()
	return nil
}

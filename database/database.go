// This package has database database layer. Never deal with DDL construction.
package database

import (
	"bytes"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	DbName        string
	User          string
	Password      string
	Host          string
	Port          int
	Socket        string
	SkipView      bool
	SkipExtension bool

	// Only MySQL
	MySQLEnableCleartextPlugin bool
	SslMode                    string
	SslCa                      string

	// Only PostgreSQL
	TargetSchema []string

	// Only MySQL and PostgreSQL
	DumpConcurrency int
}

type GeneratorConfig struct {
	TargetTables    []string
	SkipTables      []string
	SkipViews       []string
	TargetSchema    []string
	Algorithm       string
	Lock            string
	DumpConcurrency int
}

// Abstraction layer for multiple kinds of databases
type Database interface {
	DumpDDLs() (string, error)
	DB() *sql.DB
	Close() error
	GetDefaultSchema() string
}

func RunDDLs(d Database, ddls []string, enableDrop bool, beforeApply string, ddlSuffix string) error {
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
		// Skip the DDL contain if enableDrop is true:
		// 1. DROP TABLE
		// 2. DROP SCHEMA
		// 3. DROP ROLE / USER
		// 4. DROP FUNCTION / PROCEDURE
		// 5. DROP TRIGGER
		// less dangerous DDLs
		// 6. DROP VIEW
		// 7. DROP INDEX
		// 8. DROP SEQUENCE
		// 9. DROP TYPE
		// 10. DROP MATERIALIZED VIEW
		if !enableDrop && (strings.Contains(ddl, "DROP TABLE") ||
			strings.Contains(ddl, "DROP) SCHEMA") ||
			strings.Contains(ddl, "DROP ROLE") ||
			strings.Contains(ddl, "DROP USER") ||
			strings.Contains(ddl, "DROP FUNCTION") ||
			strings.Contains(ddl, "DROP PROCEDURE") ||
			strings.Contains(ddl, "DROP TRIGGER") ||
			strings.Contains(ddl, "DROP VIEW") ||
			strings.Contains(ddl, "DROP MATERIALIZED VIEW") ||
			strings.Contains(ddl, "DROP INDEX") ||
			strings.Contains(ddl, "DROP SEQUENCE") ||
			strings.Contains(ddl, "DROP TYPE")) {
			fmt.Printf("-- Skipped: %s;\n", ddl)
			continue
		}
		fmt.Printf("%s;\n", ddl)
		fmt.Print(ddlSuffix)
		var err error
		if TransactionSupported(ddl) {
			_, err = transaction.Exec(ddl)
		} else {
			_, err = d.DB().Exec(ddl)
		}
		if err != nil {
			transaction.Rollback()
			return err
		}
	}
	transaction.Commit()
	return nil
}

func TransactionSupported(ddl string) bool {
	return !strings.Contains(strings.ToLower(ddl), "concurrently")
}

func ParseGeneratorConfig(configFile string) GeneratorConfig {
	if configFile == "" {
		return GeneratorConfig{}
	}

	buf, err := os.ReadFile(configFile)
	if err != nil {
		log.Fatal(err)
	}

	var config struct {
		TargetTables    string `yaml:"target_tables"`
		SkipTables      string `yaml:"skip_tables"`
		SkipViews       string `yaml:"skip_views"`
		TargetSchema    string `yaml:"target_schema"`
		Algorithm       string `yaml:"algorithm"`
		Lock            string `yaml:"lock"`
		DumpConcurrency int    `yaml:"dump_concurrency"`
	}

	dec := yaml.NewDecoder(bytes.NewReader(buf))
	dec.KnownFields(true)
	err = dec.Decode(&config)
	if err != nil {
		log.Fatal(err)
	}

	var targetTables []string
	if config.TargetTables != "" {
		targetTables = strings.Split(strings.Trim(config.TargetTables, "\n"), "\n")
	}

	var skipTables []string
	if config.SkipTables != "" {
		skipTables = strings.Split(strings.Trim(config.SkipTables, "\n"), "\n")
	}

	var skipViews []string
	if config.SkipViews != "" {
		skipViews = strings.Split(strings.Trim(config.SkipViews, "\n"), "\n")
	}

	var targetSchema []string
	if config.TargetSchema != "" {
		targetSchema = strings.Split(strings.Trim(config.TargetSchema, "\n"), "\n")
	}

	var algorithm string
	if config.Algorithm != "" {
		algorithm = strings.Trim(config.Algorithm, "\n")
	}

	var lock string
	if config.Lock != "" {
		lock = strings.Trim(config.Lock, "\n")
	}
	return GeneratorConfig{
		TargetTables:    targetTables,
		SkipTables:      skipTables,
		SkipViews:       skipViews,
		TargetSchema:    targetSchema,
		Algorithm:       algorithm,
		Lock:            lock,
		DumpConcurrency: config.DumpConcurrency,
	}
}

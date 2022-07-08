// This package has database database layer. Never deal with DDL construction.
package database

import (
	"database/sql"
	"fmt"
	"io/ioutil"
	"log"
	"strings"

	"gopkg.in/yaml.v2"
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
	SkipView                   bool
}

type GeneratorConfig struct {
	TargetTables []string
}

// Abstraction layer for multiple kinds of databases
type Database interface {
	DumpDDLs() (string, error)
	DB() *sql.DB
	Close() error
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

	buf, err := ioutil.ReadFile(configFile)
	if err != nil {
		log.Fatal(err)
	}

	var config struct {
		TargetTables string `yaml:"target_tables"`
	}
	err = yaml.UnmarshalStrict(buf, &config)
	if err != nil {
		log.Fatal(err)
	}

	var targetTables []string
	if config.TargetTables != "" {
		targetTables = strings.Split(strings.Trim(config.TargetTables, "\n"), "\n")
	}

	return GeneratorConfig{
		TargetTables: targetTables,
	}
}

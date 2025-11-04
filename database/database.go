// This package has database database layer. Never deal with DDL construction.
package database

import (
	"bytes"
	"database/sql"
	"log"
	"os"
	"strings"

	"github.com/goccy/go-yaml"
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

	// Only MySQL and PostgreSQL
	SslMode string

	// Only MySQL
	SslCa string

	// Only PostgreSQL
	TargetSchema []string

	// Only MySQL and PostgreSQL
	DumpConcurrency int

	// Only PostgreSQL, especially for Aurora DSQL limitation
	DisableDdlTransaction bool

	// Only MSSQL
	TrustedConnection bool   // Use Windows authentication
	Instance          string // Instance name
	TrustServerCert   bool   // Trust server certificate
}

type GeneratorConfig struct {
	TargetTables            []string
	SkipTables              []string
	SkipViews               []string
	TargetSchema            []string
	Algorithm               string
	Lock                    string
	DumpConcurrency         int
	ManagedRoles            []string // Roles whose privileges are managed by sqldef
	EnableDrop              bool     // Whether to enable DROP/REVOKE operations
	CreateIndexConcurrently bool     // Whether to add CONCURRENTLY to CREATE INDEX statements
	DisableDdlTransaction   bool     // Do not use a transaction for DDL statements
}

type TransactionQueries struct {
	Begin    string
	Commit   string
	Rollback string
}

// Abstraction layer for multiple kinds of databases
type Database interface {
	ExportDDLs() (string, error)
	DB() *sql.DB
	Close() error
	GetDefaultSchema() string
	SetGeneratorConfig(config GeneratorConfig)
	GetTransactionQueries() TransactionQueries
	GetConfig() Config
}

func isDryRun(d Database) bool {
	_, isDryRun := d.(*DryRunDatabase)
	return isDryRun
}

func RunDDLs(d Database, ddls []string, enableDrop bool, beforeApply string, ddlSuffix string, logger Logger) error {
	if isDryRun(d) {
		logger.Println("-- dry run --")
	} else {
		logger.Println("-- Apply --")
	}

	ddlsInTx := []string{}
	ddlsNotInTx := []string{}

	if d.GetConfig().DisableDdlTransaction {
		ddlsNotInTx = ddls
	} else {
		for _, ddl := range ddls {
			if TransactionSupported(ddl) {
				ddlsInTx = append(ddlsInTx, ddl)
			} else {
				ddlsNotInTx = append(ddlsNotInTx, ddl)
			}
		}
	}

	txQueries := d.GetTransactionQueries()

	var transaction *sql.Tx
	var err error

	if len(ddlsInTx) > 0 || len(beforeApply) > 0 {
		transaction, err = d.DB().Begin()
		if err != nil {
			return err
		}

		logger.Printf("%s;\n", txQueries.Begin)
	}

	if len(beforeApply) > 0 {
		// beforeApply is executed in transaction
		logger.Println(beforeApply)
		if _, err := transaction.Exec(beforeApply); err != nil {
			_ = transaction.Rollback()
			logger.Printf("%s;\n", txQueries.Rollback)
			return err
		}
	}

	// DDLs in transaction
	for _, ddl := range ddlsInTx {
		// Skip the DDL that contains destructive operations unless enableDrop.
		if !enableDrop && IsDropStatement(ddl) {
			logger.Printf("-- Skipped: %s;\n", ddl)
			continue
		}
		logger.Printf("%s;\n", ddl)
		logger.Print(ddlSuffix)
		_, err = transaction.Exec(ddl)
		if err != nil {
			_ = transaction.Rollback()
			logger.Printf("%s;\n", txQueries.Rollback)
			return err
		}
	}

	// Only commit if we started a transaction
	if transaction != nil {
		if err := transaction.Commit(); err != nil {
			return err
		}
		logger.Printf("%s;\n", txQueries.Commit)
	}

	// DDLs not in transaction
	for _, ddl := range ddlsNotInTx {
		// Skip the DDL that contains destructive operations unless enableDrop.
		if !enableDrop && IsDropStatement(ddl) {
			logger.Printf("-- Skipped: %s;\n", ddl)
			continue
		}

		logger.Printf("%s;\n", ddl)
		logger.Print(ddlSuffix)
		_, err = d.DB().Exec(ddl)
		if err != nil {
			return err
		}
	}
	return nil
}

func TransactionSupported(ddl string) bool {
	ddlLower := strings.ToLower(ddl)
	return !strings.Contains(ddlLower, "concurrently") && !strings.Contains(ddlLower, "async")
}

func IsDropStatement(ddl string) bool {
	return strings.Contains(ddl, "DROP TABLE") ||
		strings.Contains(ddl, "DROP SCHEMA") ||
		strings.Contains(ddl, "DROP COLUMN") ||
		strings.Contains(ddl, "DROP ROLE") ||
		strings.Contains(ddl, "DROP USER") ||
		strings.Contains(ddl, "DROP FUNCTION") ||
		strings.Contains(ddl, "DROP PROCEDURE") ||
		strings.Contains(ddl, "DROP TRIGGER") ||
		strings.Contains(ddl, "DROP VIEW") ||
		strings.Contains(ddl, "DROP MATERIALIZED VIEW") ||
		strings.Contains(ddl, "DROP INDEX") ||
		strings.Contains(ddl, "DROP SEQUENCE") ||
		strings.Contains(ddl, "DROP TYPE")
}

func MergeGeneratorConfigs(configs []GeneratorConfig) GeneratorConfig {
	var result GeneratorConfig
	for _, config := range configs {
		result = MergeGeneratorConfig(result, config)
	}
	return result
}

func ParseGeneratorConfigString(yamlString string) GeneratorConfig {
	if yamlString == "" {
		return GeneratorConfig{}
	}
	return parseGeneratorConfigFromBytes([]byte(yamlString))
}

func ParseGeneratorConfig(configFile string) GeneratorConfig {
	if configFile == "" {
		return GeneratorConfig{}
	}

	buf, err := os.ReadFile(configFile)
	if err != nil {
		log.Fatal(err)
	}
	return parseGeneratorConfigFromBytes(buf)
}

// MergeGeneratorConfig merges two configs, with the second one taking precedence
func MergeGeneratorConfig(base, override GeneratorConfig) GeneratorConfig {
	result := base

	// Override fields if they are set in the override config
	if override.TargetTables != nil {
		result.TargetTables = override.TargetTables
	}
	if override.SkipTables != nil {
		result.SkipTables = override.SkipTables
	}
	if override.SkipViews != nil {
		result.SkipViews = override.SkipViews
	}
	if override.TargetSchema != nil {
		result.TargetSchema = override.TargetSchema
	}
	if override.Algorithm != "" {
		result.Algorithm = override.Algorithm
	}
	if override.Lock != "" {
		result.Lock = override.Lock
	}
	if override.DumpConcurrency != 0 {
		result.DumpConcurrency = override.DumpConcurrency
	}
	if override.ManagedRoles != nil {
		result.ManagedRoles = override.ManagedRoles
	}
	if override.EnableDrop {
		result.EnableDrop = override.EnableDrop
	}
	if override.CreateIndexConcurrently {
		result.CreateIndexConcurrently = override.CreateIndexConcurrently
	}
	if override.DisableDdlTransaction {
		result.DisableDdlTransaction = override.DisableDdlTransaction
	}

	return result
}

func parseGeneratorConfigFromBytes(buf []byte) GeneratorConfig {
	var config struct {
		TargetTables            string   `yaml:"target_tables"`
		SkipTables              string   `yaml:"skip_tables"`
		SkipViews               string   `yaml:"skip_views"`
		TargetSchema            string   `yaml:"target_schema"`
		Algorithm               string   `yaml:"algorithm"`
		Lock                    string   `yaml:"lock"`
		DumpConcurrency         int      `yaml:"dump_concurrency"`
		ManagedRoles            []string `yaml:"managed_roles"`
		EnableDrop              bool     `yaml:"enable_drop"`
		CreateIndexConcurrently bool     `yaml:"create_index_concurrently"`
		DisableDdlTransaction   bool     `yaml:"disable_ddl_transaction"`
	}

	dec := yaml.NewDecoder(bytes.NewReader(buf), yaml.DisallowUnknownField())
	err := dec.Decode(&config)
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
		TargetTables:            targetTables,
		SkipTables:              skipTables,
		SkipViews:               skipViews,
		TargetSchema:            targetSchema,
		Algorithm:               algorithm,
		Lock:                    lock,
		DumpConcurrency:         config.DumpConcurrency,
		ManagedRoles:            config.ManagedRoles,
		EnableDrop:              config.EnableDrop,
		CreateIndexConcurrently: config.CreateIndexConcurrently,
		DisableDdlTransaction:   config.DisableDdlTransaction,
	}
}

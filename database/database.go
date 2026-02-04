// This package has database database layer. Never deal with DDL construction.
package database

import (
	"bytes"
	"database/sql"
	"log"
	"os"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/sqldef/sqldef/v3/parser"
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
	SkipPartition bool

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
	ManagedRoles            []string // Roles whose privileges are managed by sqldef (empty means no privileges are managed)
	EnableDrop              bool     // Whether to enable DROP/REVOKE operations
	CreateIndexConcurrently bool     // Whether to add CONCURRENTLY to CREATE INDEX statements
	DisableDdlTransaction   bool     // Do not use a transaction for DDL statements
	LegacyIgnoreQuotes      bool     // true = ignore quotes (legacy), false = preserve quotes

	// MySQL-specific: value of lower_case_table_names server variable.
	// 0 = case-sensitive (Linux default), 1 or 2 = case-insensitive (Windows/macOS).
	// Default is 0 (case-sensitive) for offline mode compatibility.
	MysqlLowerCaseTableNames int
}

type TransactionQueries struct {
	Begin    string
	Commit   string
	Rollback string
}

// Ident is an alias for parser.Ident.
// Represents an identifier with quote information for quote-aware identifier handling.
type Ident = parser.Ident

// NewIdent is an alias for parser.NewIdent.
var NewIdent = parser.NewIdent

// NewIdentWithQuoteDetected creates an Ident with the Quoted flag inferred from content:
//   - If the name contains uppercase letters, it must have been quoted
//     (PostgreSQL folds unquoted identifiers to lowercase)
//   - If the name contains special characters (dots, spaces, etc.), it requires quoting
//   - If the name is all lowercase without special chars, it's treated as unquoted.
//     This is correct because in PostgreSQL, "users" (quoted lowercase) and users
//     (unquoted) are semantically equivalent and can be referenced interchangeably.
//
// Note: This does NOT check for reserved keywords. Keywords are handled separately
// at DDL output time because:
//   - The Quoted flag represents whether quoting is needed to preserve the identifier's form
//   - Keyword escaping is a SQL syntax requirement, not an identifier property
//
// Use this for identifiers from the database or auto-generated constraint names.
func NewIdentWithQuoteDetected(name string) Ident {
	return Ident{Name: name, Quoted: hasNonStandardChars(name)}
}

// NeedsQuoting returns true if an identifier needs to be quoted in SQL output.
// This is the complete check for DDL generation, combining:
//   - Non-standard characters (uppercase, special chars, invalid start)
//   - Reserved keywords
//
// Use this when generating SQL output to determine if quoting is required.
func NeedsQuoting(name string) bool {
	return hasNonStandardChars(name) || parser.IsKeyword(name)
}

// hasNonStandardChars returns true if an identifier contains characters
// that require quoting to preserve the identifier's form. This checks:
//   - Uppercase letters (PostgreSQL folds unquoted to lowercase)
//   - Special characters that aren't allowed in unquoted identifiers
//   - Invalid first character (must be letter or underscore)
//
// This does NOT check for reserved keywords - use NeedsQuoting for that.
func hasNonStandardChars(name string) bool {
	if name == "" {
		return false
	}
	// Check if it has uppercase letters
	if strings.ToLower(name) != name {
		return true
	}
	for i, r := range name {
		if i == 0 {
			// First character: must be letter or underscore
			if !((r >= 'a' && r <= 'z') || r == '_') {
				return true
			}
		} else {
			// Remaining characters: letters, digits, underscores, or $ are allowed
			if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '$') {
				return true
			}
		}
	}
	return false
}

// NewNormalizedIdent normalizes an Ident for comparison:
//   - Quoted identifiers: preserve case, set Quoted based on whether name has uppercase
//   - Unquoted identifiers: normalize to lowercase, set Quoted=false
func NewNormalizedIdent(ident Ident) Ident {
	if ident.Quoted {
		return NewIdentWithQuoteDetected(ident.Name)
	}
	return Ident{Name: strings.ToLower(ident.Name), Quoted: false}
}

// QualifiedName represents a schema-qualified table name with quote information.
type QualifiedName struct {
	Schema Ident // empty if not specified (will use default schema)
	Name   Ident
}

// IsEmpty returns true if the qualified name has no name set.
func (q QualifiedName) IsEmpty() bool {
	return q.Name.IsEmpty()
}

// RawString returns the raw qualified name as "schema.name" or just "name" if no schema.
// This is NOT escaped for SQL output and NOT normalized for comparison.
// Use this for logging, debugging, or map keys.
func (q QualifiedName) RawString() string {
	if q.Schema.IsEmpty() {
		return q.Name.Name
	}
	return q.Schema.Name + "." + q.Name.Name
}

// Abstraction layer for multiple kinds of databases
type Database interface {
	ExportDDLs() (string, error)
	DB() *sql.DB
	Close() error
	GetDefaultSchema() string
	SetGeneratorConfig(config GeneratorConfig)
	GetGeneratorConfig() GeneratorConfig
	GetTransactionQueries() TransactionQueries
	GetConfig() Config
}

func isDryRun(d Database) bool {
	_, isDryRun := d.(*DryRunDatabase)
	return isDryRun
}

func isSingleLineComment(s string) bool {
	return strings.HasPrefix(s, "-- ") && !strings.Contains(strings.TrimSpace(s), "\n")
}

func RunDDLs(d Database, ddls []string, beforeApply string, ddlSuffix string, logger Logger) error {
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
		logger.Printf("%s;\n", ddl)

		if isSingleLineComment(ddl) {
			// Skip commented DDLs (e.g., "-- Skipped: ...")
			continue
		}

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
		logger.Printf("%s;\n", ddl)
		// Skip ddlSuffix and execution for commented DDLs (e.g., "-- Skipped: ...")
		if !isSingleLineComment(ddl) {
			logger.Print(ddlSuffix)
			_, err = d.DB().Exec(ddl)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func TransactionSupported(ddl string) bool {
	ddlLower := strings.ToLower(ddl)
	return !strings.Contains(ddlLower, "concurrently") && !strings.Contains(ddlLower, "async")
}

func MergeGeneratorConfigs(configs []GeneratorConfig) GeneratorConfig {
	var result GeneratorConfig
	for _, config := range configs {
		result = MergeGeneratorConfig(result, config)
	}
	return result
}

func ParseGeneratorConfigString(yamlString string, defaults GeneratorConfig) GeneratorConfig {
	if yamlString == "" {
		return defaults
	}
	return parseGeneratorConfigFromBytes([]byte(yamlString), defaults)
}

func ParseGeneratorConfig(configFile string, defaults GeneratorConfig) GeneratorConfig {
	if configFile == "" {
		return defaults
	}

	buf, err := os.ReadFile(configFile)
	if err != nil {
		log.Fatal(err)
	}
	return parseGeneratorConfigFromBytes(buf, defaults)
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
	// LegacyIgnoreQuotes: override always takes precedence (set by first config with database-specific default)
	result.LegacyIgnoreQuotes = override.LegacyIgnoreQuotes

	return result
}

func parseGeneratorConfigFromBytes(buf []byte, defaults GeneratorConfig) GeneratorConfig {
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
		LegacyIgnoreQuotes      *bool    `yaml:"legacy_ignore_quotes"`
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

	// Use the provided default, override if explicitly set in config
	legacyIgnoreQuotes := defaults.LegacyIgnoreQuotes
	if config.LegacyIgnoreQuotes != nil {
		legacyIgnoreQuotes = *config.LegacyIgnoreQuotes
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
		LegacyIgnoreQuotes:      legacyIgnoreQuotes,
	}
}

// This package has database database layer. Never deal with DDL construction.
package database

import (
	"bytes"
	"context"
	"database/sql"
	"log"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/goccy/go-yaml"
	"github.com/sqldef/sqldef/v3/parser"
	"github.com/sqldef/sqldef/v3/util"
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
	BulkAlter               bool     // Bundle multiple ALTER TABLE actions on the same table into a single statement (MySQL only)
	LegacyIgnoreQuotes      bool     // true = ignore quotes (legacy), false = preserve quotes

	ManageExtensions *[]ManageObjectRule
	ManagePrivileges *[]ManageObjectRule // manage.privilege rules: which grantees' privileges are managed and whether REVOKE is allowed
	ManageFunctions  *[]ManageObjectRule // manage.function rules: which functions are managed and whether DROP is allowed
	ManageOwners     *[]ManageObjectRule // manage.owner rules: which owner roles are managed

	// MySQL-specific: value of lower_case_table_names server variable.
	// 0 = case-sensitive (Linux default), 1 or 2 = case-insensitive (Windows/macOS).
	// Default is 0 (case-sensitive) for offline mode compatibility.
	MysqlLowerCaseTableNames int

	// PostgreSQL-specific: the default operator class of every access method, keyed by
	// "<access method>.<operator class>" in lower case, e.g. "btree.text_ops".
	// The database omits the default operator class from the DDL it exports, so one written
	// explicitly in the desired DDL has to compare equal to an omitted one.
	// Nil in offline mode, where an explicit operator class is compared as-is.
	// Every copy of the config shares one map that ExportDDLs() refills, so read it only after
	// exporting: the schema being applied may install an extension that registers operator classes.
	PostgresDefaultOperatorClasses map[string]bool
}

type ManageObjectRule struct {
	Target string `yaml:"target"`
	Drop   bool   `yaml:"drop"`
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

	// SessionSetupQueries returns the statements to run on the connection that applies the
	// DDLs, before the first of them. They configure the session, so they must not be run on
	// a connection the pool may hand to someone else.
	SessionSetupQueries() []string
}

func isDryRun(d Database) bool {
	_, isDryRun := d.(*DryRunDatabase)
	return isDryRun
}

// isCommentedOut reports whether a DDL consists only of "--" comment lines
// (e.g. a statement commented out as "-- Skipped: ..."), and therefore must
// not be executed. A multi-line statement counts only if every non-empty line
// is a comment, so partially commented text is still executed (and fails)
// rather than being silently ignored.
func isCommentedOut(s string) bool {
	if !strings.HasPrefix(s, "-- ") {
		return false
	}
	for line := range strings.SplitSeq(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "--") {
			return false
		}
	}
	return true
}

func RunDDLs(d Database, ddls []string, beforeApply string, ddlSuffix string, logger Logger) error {
	if isDryRun(d) {
		logger.Println("-- dry run --")
	} else {
		logger.Println("-- Apply --")
	}

	// Every statement runs on one connection: a session setting made by SessionSetupQueries or
	// by beforeApply has to still be in effect when a later statement runs, and the pool would
	// otherwise hand out a connection that never saw it.
	conn, err := d.DB().Conn(context.Background())
	if err != nil {
		return err
	}
	defer conn.Close()

	runner := ddlRunner{conn: conn, txQueries: d.GetTransactionQueries(), ddlSuffix: ddlSuffix, logger: logger}

	for _, query := range d.SessionSetupQueries() {
		if _, err := conn.ExecContext(context.Background(), query); err != nil {
			return err
		}
	}

	if len(beforeApply) > 0 {
		if err := runner.begin(); err != nil {
			return err
		}
		logger.Println(beforeApply)
		if _, err := runner.transaction.Exec(beforeApply); err != nil {
			runner.rollback()
			return err
		}
	}

	// A statement is executed in the order it was generated: a later statement may depend on
	// an earlier one (a COMMENT ON INDEX, or a foreign key over a unique index). Statements
	// that cannot run inside a transaction, such as CREATE INDEX CONCURRENTLY, therefore end
	// the transaction that precedes them rather than being deferred to the end.
	inTransaction := !d.GetConfig().DisableDdlTransaction
	for _, ddl := range ddls {
		// A statement the generator commented out is only printed, so it must not decide
		// whether the transaction around it stays open.
		if isCommentedOut(ddl) {
			logger.Printf("%s;\n", ddl)
			continue
		}

		if inTransaction && TransactionSupported(ddl) {
			if err := runner.begin(); err != nil {
				return err
			}
		} else if err := runner.commit(); err != nil {
			return err
		}

		if err := runner.exec(ddl); err != nil {
			return err
		}
	}

	return runner.commit()
}

// ddlRunner executes DDLs on one connection, keeping at most one open transaction.
type ddlRunner struct {
	conn        *sql.Conn
	txQueries   TransactionQueries
	ddlSuffix   string
	logger      Logger
	transaction *sql.Tx
}

func (r *ddlRunner) begin() error {
	if r.transaction != nil {
		return nil
	}
	transaction, err := r.conn.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	r.transaction = transaction
	r.logger.Printf("%s;\n", r.txQueries.Begin)
	return nil
}

func (r *ddlRunner) commit() error {
	if r.transaction == nil {
		return nil
	}
	transaction := r.transaction
	r.transaction = nil
	if err := transaction.Commit(); err != nil {
		return err
	}
	r.logger.Printf("%s;\n", r.txQueries.Commit)
	return nil
}

func (r *ddlRunner) rollback() {
	if r.transaction == nil {
		return
	}
	_ = r.transaction.Rollback()
	r.transaction = nil
	r.logger.Printf("%s;\n", r.txQueries.Rollback)
}

func (r *ddlRunner) exec(ddl string) error {
	r.logger.Printf("%s;\n", ddl)
	r.logger.Print(r.ddlSuffix)
	var err error
	if r.transaction != nil {
		_, err = r.transaction.Exec(ddl)
	} else {
		_, err = r.conn.ExecContext(context.Background(), ddl)
	}
	if err != nil {
		r.rollback()
		return err
	}
	return nil
}

// nonTransactionalDDL matches the statements PostgreSQL and Aurora DSQL refuse to run inside a
// transaction. The keyword is matched where the syntax puts it: looking for it anywhere in the
// statement would also find it in an identifier, a string literal or a comment.
var nonTransactionalDDL = regexp.MustCompile(`(?i)^\s*(?:CREATE(?:\s+UNIQUE)?\s+INDEX|DROP\s+INDEX)\s+(?:CONCURRENTLY|ASYNC)\b`)

func TransactionSupported(ddl string) bool {
	return !nonTransactionalDDL.MatchString(ddl)
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
	if override.ManageExtensions != nil {
		result.ManageExtensions = override.ManageExtensions
	}
	if override.ManageFunctions != nil {
		result.ManageFunctions = override.ManageFunctions
	}
	if override.ManagePrivileges != nil {
		result.ManagePrivileges = override.ManagePrivileges
	}
	if override.ManageOwners != nil {
		result.ManageOwners = override.ManageOwners
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
	if override.BulkAlter {
		result.BulkAlter = override.BulkAlter
	}
	// LegacyIgnoreQuotes: override always takes precedence (set by first config with database-specific default)
	result.LegacyIgnoreQuotes = override.LegacyIgnoreQuotes

	return result
}

func parseGeneratorConfigFromBytes(buf []byte, defaults GeneratorConfig) GeneratorConfig {
	var config struct {
		TargetTables            string                     `yaml:"target_tables"`
		SkipTables              string                     `yaml:"skip_tables"`
		SkipViews               string                     `yaml:"skip_views"`
		TargetSchema            string                     `yaml:"target_schema"`
		Algorithm               string                     `yaml:"algorithm"`
		Lock                    string                     `yaml:"lock"`
		DumpConcurrency         int                        `yaml:"dump_concurrency"`
		ManagedRoles            []string                   `yaml:"managed_roles"`
		EnableDrop              bool                       `yaml:"enable_drop"`
		CreateIndexConcurrently bool                       `yaml:"create_index_concurrently"`
		DisableDdlTransaction   bool                       `yaml:"disable_ddl_transaction"`
		BulkAlter               bool                       `yaml:"bulk_alter"`
		LegacyIgnoreQuotes      *bool                      `yaml:"legacy_ignore_quotes"`
		Manage                  map[string]yaml.RawMessage `yaml:"manage"`
	}

	dec := yaml.NewDecoder(bytes.NewReader(buf), yaml.DisallowUnknownField())
	err := dec.Decode(&config)
	if err != nil {
		log.Fatal(err)
	}

	manageExtensions := parseManageRules(config.Manage, "extension")
	manageFunctions := parseManageRules(config.Manage, "function")
	managePrivileges := parseManageRules(config.Manage, "privilege")
	manageOwners := parseManageRules(config.Manage, "owner")

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
		BulkAlter:               config.BulkAlter,
		LegacyIgnoreQuotes:      legacyIgnoreQuotes,
		ManageExtensions:        manageExtensions,
		ManageFunctions:         manageFunctions,
		ManagePrivileges:        managePrivileges,
		ManageOwners:            manageOwners,
	}
}

// manageKnownKeys are the object-type keys defined by the manage: RFC (object-management.md).
// The keys in manageImplementedKeys work; the rest are recognized-but-not-yet-implemented and get
// a warning. Any other key is a typo, not a forward-compatibility case, and is a hard error.
var manageKnownKeys = map[string]bool{
	"schema": true, "table": true, "view": true, "materialized_view": true,
	"index": true, "function": true, "procedure": true, "trigger": true,
	"sequence": true, "type": true, "domain": true, "policy": true,
	"extension": true, "privilege": true, "owner": true,
}

// compiledManageTargets memoizes CompileManageTarget. Targets are matched once per object in the
// database and the set of distinct patterns is tiny, so compiling on every call dominates the cost.
var compiledManageTargets sync.Map

// CompileManageTarget compiles a manage: rule's target pattern into the anchored regexp
// used to match object names, wrapped in a non-capturing group so alternation (a|b) anchors
// correctly on both ends.
func CompileManageTarget(target string) (*regexp.Regexp, error) {
	if cached, ok := compiledManageTargets.Load(target); ok {
		return cached.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile("^(?:" + target + ")$")
	if err != nil {
		return nil, err
	}
	compiledManageTargets.Store(target, re)
	return re, nil
}

// manageImplementedKeys are the manage: keys with a working implementation; the other
// recognized keys are parsed permissively and ignored with a warning.
var manageImplementedKeys = map[string]bool{
	"extension": true,
	"function":  true,
	"privilege": true,
	"owner":     true,
}

func parseManageRules(manage map[string]yaml.RawMessage, want string) *[]ManageObjectRule {
	if manage == nil {
		return nil
	}

	var result *[]ManageObjectRule
	for key, raw := range util.CanonicalMapIter(manage) {
		if key != want {
			if !manageKnownKeys[key] {
				log.Fatalf("manage.%s is not a recognized manage: key (typo?)", key)
			}
			if !manageImplementedKeys[key] && want == "extension" { // warn once, not per implemented key
				slog.Warn("manage key is not yet supported and will be ignored; only manage.extension, manage.function, manage.privilege and manage.owner are currently implemented", "key", key)
			}
			continue
		}

		rules := []ManageObjectRule{}
		if len(bytes.TrimSpace(raw)) > 0 {
			dec := yaml.NewDecoder(bytes.NewReader(raw), yaml.DisallowUnknownField())
			if err := dec.Decode(&rules); err != nil {
				log.Fatal(err)
			}
		}
		for _, rule := range rules {
			if want == "owner" && rule.Drop {
				slog.Warn("manage.owner: drop has no meaning for owners and is ignored", "target", rule.Target)
			}
			if rule.Target == "" {
				continue
			}
			if _, err := CompileManageTarget(rule.Target); err != nil {
				log.Fatalf("manage.%s: invalid target regexp %q: %s", want, rule.Target, err)
			}
		}
		result = &rules
	}
	return result
}

// MatchManageObjectRule returns the first rule whose target matches name (first match
// wins). An empty rules list matches everything with drop disabled; an empty target
// matches everything. The second return reports whether any rule matched.
func MatchManageObjectRule(rules []ManageObjectRule, name string) (ManageObjectRule, bool) {
	if len(rules) == 0 {
		return ManageObjectRule{Drop: false}, true
	}
	for _, rule := range rules {
		if rule.Target == "" {
			return rule, true
		}
		re, err := CompileManageTarget(rule.Target)
		if err == nil && re.MatchString(name) {
			return rule, true
		}
	}
	return ManageObjectRule{}, false
}

// ManagesOwners reports whether object ownership is diffed at all.
//
// manage.owner turns it on explicitly. Before it existed, ownership rode along with managed_roles,
// because that was the only mode in which --export emitted owners. That fallback stays alive for
// exactly as long as managed_roles itself does: manage.privilege is what supersedes it, here as in
// isManagedGrantee.
func (config *GeneratorConfig) ManagesOwners() bool {
	if config.ManageOwners != nil {
		return true
	}
	return config.ManagePrivileges == nil && len(config.ManagedRoles) > 0
}

// ManagesOwnerRole reports whether ownership by the given role is managed. The legacy
// managed_roles path has no owner patterns to match, so it manages every role.
func (config *GeneratorConfig) ManagesOwnerRole(role string) bool {
	if !config.ManagesOwners() {
		return false
	}
	if config.ManageOwners == nil {
		return true
	}
	_, matched := MatchManageObjectRule(*config.ManageOwners, role)
	return matched
}

package testutils

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/goccy/go-yaml"
	"github.com/sqldef/sqldef/v3/database"
	"github.com/sqldef/sqldef/v3/schema"
	"github.com/sqldef/sqldef/v3/util"
	"github.com/stretchr/testify/assert"
)

type TestCase struct {
	Current            string  // default: empty schema
	Desired            string  // default: empty schema
	Up                 *string // expected DDL for current → desired migration
	Down               *string // expected DDL for desired → current migration
	Output             *string `yaml:"output,omitempty"` // DEPRECATED: use 'up' and 'down' instead
	Error              *string // default: nil
	MinVersion         string  `yaml:"min_version"`
	MaxVersion         string  `yaml:"max_version"`
	User               string
	Flavor             string   // database flavor (e.g., "mariadb", "mysql")
	ManagedRoles       []string `yaml:"managed_roles"`        // Roles whose privileges are managed by sqldef
	EnableDrop         *bool    `yaml:"enable_drop"`          // Whether to enable DROP/REVOKE operations
	LegacyIgnoreQuotes *bool    `yaml:"legacy_ignore_quotes"` // nil or true = ignore quotes (legacy default), false = preserve quotes
	Offline            bool     `yaml:"offline"`
	Config             struct { // Optional config settings for the test
		CreateIndexConcurrently bool `yaml:"create_index_concurrently"`
		DisableDdlTransaction   bool `yaml:"disable_ddl_transaction"`
	} `yaml:"config"`
}

func init() {
	util.InitSlog()

	// In test environments, suppress INFO-level logs to prevent them from contaminating test output comparisons.
	// Users can still see DEBUG/INFO logs by setting LOG_LEVEL=debug or LOG_LEVEL=info environment variable,
	// which will override this default. Warnings and errors will still appear by default.
	if os.Getenv("LOG_LEVEL") == "" {
		// Set default test log level to WARN to hide INFO messages like "Using generic parser only mode"
		opts := &slog.HandlerOptions{
			Level: slog.LevelWarn,
		}
		handler := slog.NewTextHandler(os.Stderr, opts)
		slog.SetDefault(slog.New(handler))
	}
}

// CreateTestDatabaseName generates a unique database name for a test case.
// The name is sanitized to be a valid database name (lowercase, alphanumeric + underscore)
// and uses MD5 hash to ensure uniqueness.
//
// Parameters:
//   - testName: The test name to sanitize
//   - dbLimit: Database name length limit. For example:
//   - PostgreSQL: 63 characters
//   - SQL Server: 128 characters
//
// The resulting format is: sqldef_test_{sanitized}_{hash}
// where hash is the first 8 characters of the MD5 hash of the original test name.
func CreateTestDatabaseName(testName string, dbLimit int) string {
	const prefix = "sqldef_test_"
	const hashLen = 8

	// Calculate maximum length for the sanitized portion
	// dbLimit = len(prefix) + len(sanitized) + len("_") + len(hash)
	// sanitized = dbLimit - len(prefix) - 1 - hashLen
	maxSanitizedLen := dbLimit - len(prefix) - 1 - hashLen

	// Sanitize the test name: lowercase, replace non-alphanumeric with underscore
	sanitized := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) {
			return unicode.ToLower(r)
		}
		if unicode.IsDigit(r) {
			return r
		}
		return '_'
	}, testName)

	// Truncate to maxSanitizedLen to ensure the full name stays within database limits
	if len(sanitized) > maxSanitizedLen {
		sanitized = sanitized[:maxSanitizedLen]
	}

	// Create a short hash from the full test name for uniqueness
	hash := md5.Sum([]byte(testName))
	hashStr := hex.EncodeToString(hash[:])[:hashLen]

	return fmt.Sprintf("%s%s_%s", prefix, sanitized, hashStr)
}

func ReadTests(pattern string) (map[string]TestCase, error) {
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	ret := map[string]TestCase{}
	for _, file := range files {
		var tests map[string]*TestCase

		buf, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}

		dec := yaml.NewDecoder(bytes.NewReader(buf), yaml.DisallowUnknownField())
		err = dec.Decode(&tests)
		if err != nil {
			return nil, err
		}

		for name, test := range tests {
			// Check for deprecated 'output' field
			if test.Output != nil {
				log.Fatalf("Test case '%s': The 'output' field is deprecated. Please use 'up' and 'down' instead.\n\nMigration guide:\n  output: |     →  up: |\n    DDL...            DDL...\n                      down: |\n                        REVERSE_DDL...", name)
			}

			// Validate up/down dependency: both must be present or both must be absent
			if (test.Up != nil && test.Down == nil) || (test.Up == nil && test.Down != nil) {
				log.Fatalf("Test case '%s': If 'up' is specified, 'down' must also be specified (and vice versa).\nFor idempotency-only tests, omit both 'up' and 'down'.", name)
			}

			if test.EnableDrop == nil {
				enableDrop := true // defaults to true
				test.EnableDrop = &enableDrop
			}
			if _, ok := ret[name]; ok {
				log.Fatalf("There are multiple test cases named '%s'", name)
			}
			ret[name] = *test
		}
	}

	return ret, nil
}

func RunTest(t *testing.T, db database.Database, test TestCase, mode schema.GeneratorMode, sqlParser database.Parser, version string, allowedFlavor string) {
	t.Helper()

	if test.MinVersion != "" && compareVersion(t, version, test.MinVersion) < 0 {
		t.Skipf("Version '%s' is smaller than min_version '%s'", version, test.MaxVersion)
	}
	if test.MaxVersion != "" && compareVersion(t, version, test.MaxVersion) > 0 {
		t.Skipf("Version '%s' is larger than max_version '%s'", version, test.MaxVersion)
	}
	// If test requires a specific flavor, check if it matches the current environment
	if test.Flavor != "" {
		// If no flavor is explicitly set, default to "mysql" for MySQL tests
		currentFlavor := allowedFlavor
		if currentFlavor == "" && mode == schema.GeneratorModeMysql {
			currentFlavor = "mysql"
		}
		if test.Flavor != currentFlavor {
			t.Skipf("Test flavor '%s' does not match current flavor '%s'", test.Flavor, currentFlavor)
		}
	}

	// Determine LegacyIgnoreQuotes: use test value if specified, otherwise default to true (legacy mode)
	legacyIgnoreQuotes := true // default: true for backward compatibility
	if test.LegacyIgnoreQuotes != nil {
		legacyIgnoreQuotes = *test.LegacyIgnoreQuotes
	}

	config := database.GeneratorConfig{
		ManagedRoles:            test.ManagedRoles,
		EnableDrop:              *test.EnableDrop,
		CreateIndexConcurrently: test.Config.CreateIndexConcurrently,
		DisableDdlTransaction:   test.Config.DisableDdlTransaction,
		LegacyIgnoreQuotes:      legacyIgnoreQuotes,
	}

	// Set config first to populate database-specific settings (e.g., MysqlLowerCaseTableNames)
	db.SetGeneratorConfig(config)
	config = db.GetGeneratorConfig()

	if test.Offline {
		RunOfflineTest(t, test, mode, sqlParser, config, db.GetDefaultSchema())
		return
	}

	if test.Current != "" {
		ddls, err := splitDDLs(mode, sqlParser, test.Current, db.GetDefaultSchema(), legacyIgnoreQuotes, config.MysqlLowerCaseTableNames)
		if err != nil {
			t.Fatal(err)
		}
		err = runDDLs(db, ddls, *test.EnableDrop)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Test idempotency of current schema
	currentDDLs, err := db.ExportDDLs()
	if err != nil {
		log.Fatal(err)
	}
	ddls, err := schema.GenerateIdempotentDDLs(mode, sqlParser, test.Current, currentDDLs, config, db.GetDefaultSchema())
	if err != nil {
		t.Fatal(err)
	}
	if len(ddls) > 0 {
		t.Errorf("Current schema is not idempotent. Expected no changes when reapplying current schema, but got:\n```\n%s```\nThis means the current schema state didn't apply correctly or has conflicting/duplicate statements.", joinDDLs(ddls))
	}

	// Main test
	currentDDLs, err = db.ExportDDLs()
	if err != nil {
		log.Fatal(err)
	}

	if test.Up != nil && test.Down != nil {
		// Bidirectional migration test
		// Test forward migration: current → desired should produce Up
		ddls, err = schema.GenerateIdempotentDDLs(mode, sqlParser, test.Desired, currentDDLs, config, db.GetDefaultSchema())

		// Handle expected errors
		if test.Error != nil {
			if err == nil {
				t.Errorf("expected error: %s, but got no error", *test.Error)
			} else if err.Error() != *test.Error {
				t.Errorf("expected error: %s, but got: %s", *test.Error, err.Error())
			}
			return
		}

		if err != nil {
			t.Fatal(err)
		}

		expected := strings.TrimSpace(*test.Up)
		actual := strings.TrimSpace(joinDDLs(ddls))
		assert.Equal(t, expected, actual, "Forward migration (up) output doesn't match expected.")

		err = runDDLs(db, ddls, *test.EnableDrop)
		if err != nil {
			t.Fatal(err)
		}

		// Test idempotency of desired schema
		currentDDLs, err = db.ExportDDLs()
		if err != nil {
			log.Fatal(err)
		}
		ddls, err = schema.GenerateIdempotentDDLs(mode, sqlParser, test.Desired, currentDDLs, config, db.GetDefaultSchema())
		if err != nil {
			t.Fatal(err)
		}
		if len(ddls) > 0 {
			t.Errorf("Desired schema is not idempotent. Expected no changes when reapplying desired schema, but got:\n```\n%s```\nThis means the forward migration didn't apply correctly.", joinDDLs(ddls))
		}

		// Test reverse migration: desired → current should produce Down
		currentDDLs, err = db.ExportDDLs()
		if err != nil {
			log.Fatal(err)
		}
		ddls, err = schema.GenerateIdempotentDDLs(mode, sqlParser, test.Current, currentDDLs, config, db.GetDefaultSchema())
		if err != nil {
			t.Fatal(err)
		}

		expected = strings.TrimSpace(*test.Down)
		actual = strings.TrimSpace(joinDDLs(ddls))
		assert.Equal(t, expected, actual, "Reverse migration (down) output doesn't match expected.")

		err = runDDLs(db, ddls, *test.EnableDrop)
		if err != nil {
			t.Fatal(err)
		}

		// Test idempotency of current schema after reverse migration
		currentDDLs, err = db.ExportDDLs()
		if err != nil {
			log.Fatal(err)
		}
		ddls, err = schema.GenerateIdempotentDDLs(mode, sqlParser, test.Current, currentDDLs, config, db.GetDefaultSchema())
		if err != nil {
			t.Fatal(err)
		}
		if len(ddls) > 0 {
			t.Errorf("Current schema is not idempotent after reverse migration. Expected no changes when reapplying current schema, but got:\n```\n%s```\nThis means the reverse migration didn't apply correctly.", joinDDLs(ddls))
		}
	} else {
		// Idempotency-only test (neither up nor down specified)
		ddls, err = schema.GenerateIdempotentDDLs(mode, sqlParser, test.Desired, currentDDLs, config, db.GetDefaultSchema())

		// Handle expected errors
		if test.Error != nil {
			if err == nil {
				t.Errorf("expected error: %s, but got no error", *test.Error)
			} else if err.Error() != *test.Error {
				t.Errorf("expected error: %s, but got: %s", *test.Error, err.Error())
			}
			return
		}

		if err != nil {
			t.Fatal(err)
		}

		// For idempotency-only tests, we expect no DDLs (or just apply and test idempotency)
		err = runDDLs(db, ddls, *test.EnableDrop)
		if err != nil {
			t.Fatal(err)
		}

		// Test idempotency of desired schema
		currentDDLs, err = db.ExportDDLs()
		if err != nil {
			log.Fatal(err)
		}
		ddls, err = schema.GenerateIdempotentDDLs(mode, sqlParser, test.Desired, currentDDLs, config, db.GetDefaultSchema())
		if err != nil {
			t.Fatal(err)
		}
		if len(ddls) > 0 {
			t.Errorf("Desired schema is not idempotent. Expected no changes when reapplying desired schema, but got:\n```\n%s```\nThis means the migration didn't apply correctly.", joinDDLs(ddls))
		}
	}
}

func RunOfflineTest(t *testing.T, test TestCase, mode schema.GeneratorMode, sqlParser database.Parser, config database.GeneratorConfig, defaultSchema string) {
	t.Helper()

	currentDDLs := test.Current

	if test.Up != nil && test.Down != nil {
		// Bidirectional migration test
		// Test forward migration: current → desired should produce Up
		ddls, err := schema.GenerateIdempotentDDLs(mode, sqlParser, test.Desired, currentDDLs, config, defaultSchema)

		if test.Error != nil {
			if err == nil {
				t.Errorf("expected error: %s, but got no error", *test.Error)
			} else if err.Error() != *test.Error {
				t.Errorf("expected error: %s, but got: %s", *test.Error, err.Error())
			}
			return
		}

		if err != nil {
			t.Fatal(err)
		}

		expected := strings.TrimSpace(*test.Up)
		actual := strings.TrimSpace(joinDDLs(ddls))
		assert.Equal(t, expected, actual, "Forward migration (up) output doesn't match expected.")

		// Test idempotency of desired schema
		ddls, err = schema.GenerateIdempotentDDLs(mode, sqlParser, test.Desired, test.Desired, config, defaultSchema)
		if err != nil {
			t.Fatal(err)
		}
		if len(ddls) > 0 {
			t.Errorf("Desired schema is not idempotent. Expected no changes when comparing desired to itself, but got:\n```\n%s```", joinDDLs(ddls))
		}

		// Test reverse migration: desired → current should produce Down
		ddls, err = schema.GenerateIdempotentDDLs(mode, sqlParser, test.Current, test.Desired, config, defaultSchema)
		if err != nil {
			t.Fatal(err)
		}

		expected = strings.TrimSpace(*test.Down)
		actual = strings.TrimSpace(joinDDLs(ddls))
		assert.Equal(t, expected, actual, "Reverse migration (down) output doesn't match expected.")

		// Test idempotency of current schema
		ddls, err = schema.GenerateIdempotentDDLs(mode, sqlParser, test.Current, test.Current, config, defaultSchema)
		if err != nil {
			t.Fatal(err)
		}
		if len(ddls) > 0 {
			t.Errorf("Current schema is not idempotent. Expected no changes when comparing current to itself, but got:\n```\n%s```", joinDDLs(ddls))
		}
	} else {
		// Idempotency-only test (neither up nor down specified)
		ddls, err := schema.GenerateIdempotentDDLs(mode, sqlParser, test.Desired, currentDDLs, config, defaultSchema)

		if test.Error != nil {
			if err == nil {
				t.Errorf("expected error: %s, but got no error", *test.Error)
			} else if err.Error() != *test.Error {
				t.Errorf("expected error: %s, but got: %s", *test.Error, err.Error())
			}
			return
		}

		if err != nil {
			t.Fatal(err)
		}

		// Test idempotency of desired schema
		ddls, err = schema.GenerateIdempotentDDLs(mode, sqlParser, test.Desired, test.Desired, config, defaultSchema)
		if err != nil {
			t.Fatal(err)
		}
		if len(ddls) > 0 {
			t.Errorf("Desired schema is not idempotent. Expected no changes when comparing desired to itself, but got:\n```\n%s```", joinDDLs(ddls))
		}
	}
}

// left < right: compareVersion() < 0
// left = right: compareVersion() = 0
// left > right: compareVersion() > 0
func compareVersion(t *testing.T, leftVersion string, rightVersion string) int {
	leftVersions := strings.Split(leftVersion, ".")
	rightVersions := strings.Split(rightVersion, ".")

	// Compare only specified segments
	length := min(len(leftVersions), len(rightVersions))

	for i := range length {
		left, err := strconv.Atoi(leftVersions[i])
		if err != nil {
			t.Fatal(err)
		}
		right, err := strconv.Atoi(rightVersions[i])
		if err != nil {
			t.Fatal(err)
		}

		if left < right {
			return -1
		} else if left > right {
			return 1
		}
	}
	return 0
}

func splitDDLs(mode schema.GeneratorMode, sqlParser database.Parser, str string, defaultSchema string, legacyIgnoreQuotes bool, mysqlLowerCaseTableNames int) ([]string, error) {
	statements, err := schema.ParseDDLs(mode, sqlParser, str, defaultSchema)
	if err != nil {
		return nil, err
	}

	statements = schema.SortTablesByDependencies(statements, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames)

	var ddls []string
	for _, statement := range statements {
		ddls = append(ddls, statement.Statement())
	}
	return ddls, nil
}

func runDDLs(db database.Database, ddls []string, enableDrop bool) error {
	var logger database.Logger
	if !testing.Verbose() {
		logger = database.NullLogger{}
	} else {
		logger = database.StdoutLogger{}
	}
	return database.RunDDLs(db, ddls, enableDrop /* beforeApply */, "" /* ddlSuffix */, "", logger)
}

func joinDDLs(ddls []string) string {
	var builder strings.Builder
	for _, ddl := range ddls {
		builder.WriteString(ddl)
		builder.WriteString(";\n")
	}
	return builder.String()
}

// MustExecute executes a command within a test and fails the test if it errors.
func MustExecute(t *testing.T, command string, args ...string) string {
	t.Helper()
	out, err := Execute(command, args...)
	if err != nil {
		t.Fatalf("failed to execute '%s %s' (error: '%s'): `%s`", command, strings.Join(args, " "), err, out)
	}
	return out
}

// MustExecuteNoTest executes a command and terminates the program if it errors.
// Use this in TestMain or other setup code where *testing.T is not available.
func MustExecuteNoTest(command string, args ...string) string {
	out, err := Execute(command, args...)
	if err != nil {
		log.Fatalf("failed to execute '%s %s' (error: '%s'): `%s`", command, strings.Join(args, " "), err, out)
	}
	return out
}

// BuildForTest builds the current package, adding -cover flag if GOCOVERDIR is set.
// Use this in TestMain to build binaries that support coverage collection.
func BuildForTest() {
	args := []string{"build"}
	if os.Getenv("GOCOVERDIR") != "" {
		args = append(args, "-cover")
	}
	MustExecuteNoTest("go", args...)
}

func Execute(command string, args ...string) (string, error) {
	cmd := exec.Command(command, args...)
	out, err := cmd.CombinedOutput()
	return strings.ReplaceAll(string(out), "\r\n", "\n"), err
}

type stringLogger struct {
	buf strings.Builder
}

func (l *stringLogger) Print(v ...any) {
	l.buf.WriteString(fmt.Sprint(v...))
}

func (l *stringLogger) Printf(format string, v ...any) {
	l.buf.WriteString(fmt.Sprintf(format, v...))
}

func (l *stringLogger) Println(v ...any) {
	l.buf.WriteString(fmt.Sprint(v...))
	l.buf.WriteString("\n")
}

func (l *stringLogger) String() string {
	return l.buf.String()
}

// ApplyWithOutput applies desired DDLs to a database and returns the CLI output format
// This mimics the behavior of running psqldef/mysqldef/etc from the command line
func ApplyWithOutput(db database.Database, mode schema.GeneratorMode, sqlParser database.Parser, desiredDDLs string, config database.GeneratorConfig) (string, error) {
	db.SetGeneratorConfig(config)

	currentDDLs, err := db.ExportDDLs()
	if err != nil {
		return "", err
	}

	ddls, err := schema.GenerateIdempotentDDLs(mode, sqlParser, desiredDDLs, currentDDLs, config, db.GetDefaultSchema())
	if err != nil {
		return "", err
	}

	if len(ddls) == 0 {
		return "-- Nothing is modified --\n", nil
	}

	logger := &stringLogger{}
	var ddlSuffix string
	if mode == schema.GeneratorModeMssql {
		ddlSuffix = "GO\n"
	} else {
		ddlSuffix = ""
	}

	err = database.RunDDLs(db, ddls, config.EnableDrop, "" /* beforeApply */, ddlSuffix, logger)
	if err != nil {
		return "", err
	}

	return logger.String(), nil
}

// QueryRows executes a query and returns the results as a tab-separated string.
// This is a common helper for all *Query functions in *def_test.go files.
func QueryRows(db database.Database, query string) (string, error) {
	rows, err := db.DB().Query(query)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var result strings.Builder
	columns, err := rows.Columns()
	if err != nil {
		return "", err
	}

	values := make([]any, len(columns))
	valuePtrs := make([]any, len(columns))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	for rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			return "", err
		}

		for i, val := range values {
			if i > 0 {
				result.WriteString("\t")
			}
			if val != nil {
				switch v := val.(type) {
				case []byte:
					result.WriteString(string(v))
				default:
					result.WriteString(fmt.Sprintf("%v", v))
				}
			}
		}
		result.WriteString("\n")
	}

	return result.String(), nil
}

func WriteFile(path string, content string) {
	file, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	if _, err := file.Write(([]byte)(content)); err != nil {
		log.Fatal(err)
	}
}

func StripHeredoc(heredoc string) string {
	heredoc = strings.TrimPrefix(heredoc, "\n")
	re := regexp.MustCompilePOSIX("^\t*")
	return re.ReplaceAllLiteralString(heredoc, "")
}

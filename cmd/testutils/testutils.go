// Utilities for _test.go files
package testutils

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/sqldef/sqldef/v2/database"
	"github.com/sqldef/sqldef/v2/schema"
)

type TestCase struct {
	Current      string  // default: empty schema
	Desired      string  // default: empty schema
	Output       *string // default: use Desired as Output
	Error        *string // default: nil
	MinVersion   string  `yaml:"min_version"`
	MaxVersion   string  `yaml:"max_version"`
	User         string
	Flavor       string   // database flavor (e.g., "mariadb", "mysql")
	ManagedRoles []string `yaml:"managed_roles"` // Roles whose privileges are managed by sqldef
	EnableDrop   bool     `yaml:"enable_drop"`   // Whether to enable DROP/REVOKE operations
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
			if test.Output == nil {
				test.Output = &test.Desired
			}
			if _, ok := ret[name]; ok {
				log.Fatal(fmt.Sprintf("There are multiple test cases named '%s'", name))
			}
			ret[name] = *test
		}
	}

	return ret, nil
}

func RunTest(t *testing.T, db database.Database, test TestCase, mode schema.GeneratorMode, sqlParser database.Parser, version string, allowedFlavor string) {
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

	// Prepare current
	if test.Current != "" {
		ddls, err := splitDDLs(mode, sqlParser, test.Current, db.GetDefaultSchema())
		if err != nil {
			t.Fatal(err)
		}
		err = runDDLs(db, ddls)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Set generator config on database so it knows which privileges to include
	config := database.GeneratorConfig{
		ManagedRoles: test.ManagedRoles,
		EnableDrop:   test.EnableDrop,
	}
	db.SetGeneratorConfig(config)

	// Test idempotency of current schema
	dumpDDLs, err := db.DumpDDLs()
	if err != nil {
		log.Fatal(err)
	}
	ddls, err := schema.GenerateIdempotentDDLs(mode, sqlParser, test.Current, dumpDDLs, config, db.GetDefaultSchema())
	if err != nil {
		t.Fatal(err)
	}
	if len(ddls) > 0 {
		t.Errorf("Current schema is not idempotent. Expected no changes when reapplying current schema, but got:\n```\n%s```\nThis means the current schema state didn't apply correctly or has conflicting/duplicate statements.", joinDDLs(ddls))
	}

	// Main test
	dumpDDLs, err = db.DumpDDLs()
	if err != nil {
		log.Fatal(err)
	}
	ddls, err = schema.GenerateIdempotentDDLs(mode, sqlParser, test.Desired, dumpDDLs, config, db.GetDefaultSchema())

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
	expected := *test.Output
	actual := joinDDLs(ddls)
	if expected != actual {
		t.Errorf("Migration output doesn't match expected.\n\nExpected DDLs:\n```\n%s```\n\nActual DDLs:\n```\n%s```", expected, actual)
	}
	err = runDDLs(db, ddls)
	if err != nil {
		t.Fatal(err)
	}

	// Test idempotency of desired schema
	dumpDDLs, err = db.DumpDDLs()
	if err != nil {
		log.Fatal(err)
	}
	ddls, err = schema.GenerateIdempotentDDLs(mode, sqlParser, test.Desired, dumpDDLs, config, db.GetDefaultSchema())
	if err != nil {
		t.Fatal(err)
	}
	if len(ddls) > 0 {
		t.Errorf("Desired schema is not idempotent. Expected no changes when reapplying desired schema, but got:\n```\n%s```\nThis means the migration didn't apply correctly.", joinDDLs(ddls))
	}
}

// left < right: compareVersion() < 0
// left = right: compareVersion() = 0
// left > right: compareVersion() > 0
func compareVersion(t *testing.T, leftVersion string, rightVersion string) int {
	leftVersions := strings.Split(leftVersion, ".")
	rightVersions := strings.Split(rightVersion, ".")

	// Compare only specified segments
	length := len(leftVersions)
	if length > len(rightVersions) {
		length = len(rightVersions)
	}

	for i := 0; i < length; i++ {
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

func splitDDLs(mode schema.GeneratorMode, sqlParser database.Parser, str string, defaultSchema string) ([]string, error) {
	statements, err := schema.ParseDDLs(mode, sqlParser, str, defaultSchema)
	if err != nil {
		return nil, err
	}

	var ddls []string
	for _, statement := range statements {
		ddls = append(ddls, statement.Statement())
	}
	return ddls, nil
}

func runDDLs(db database.Database, ddls []string) error {
	transaction, err := db.DB().Begin()
	if err != nil {
		return err
	}
	for _, ddl := range ddls {
		var err error
		if database.TransactionSupported(ddl) {
			_, err = transaction.Exec(ddl)
		} else {
			_, err = db.DB().Exec(ddl)
		}
		if err != nil {
			rollbackErr := transaction.Rollback()
			if rollbackErr != nil {
				return rollbackErr
			}
			return err
		}
	}
	return transaction.Commit()
}

func joinDDLs(ddls []string) string {
	var builder strings.Builder
	for _, ddl := range ddls {
		builder.WriteString(ddl)
		builder.WriteString(";\n")
	}
	return builder.String()
}

func MustExecute(command string, args ...string) string {
	out, err := Execute(command, args...)
	if err != nil {
		log.Printf("failed to execute '%s %s': `%s`", command, strings.Join(args, " "), out)
		log.Fatal(err)
	}
	return out
}

func Execute(command string, args ...string) (string, error) {
	cmd := exec.Command(command, args...)
	out, err := cmd.CombinedOutput()
	return strings.ReplaceAll(string(out), "\r\n", "\n"), err
}

package postgres

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/sqldef/sqldef/v3/database"
	"github.com/sqldef/sqldef/v3/parser"
	"github.com/stretchr/testify/assert"
)

func TestParse(t *testing.T) {
	tests, err := readTests("tests.yml")
	if err != nil {
		t.Fatal(err)
	}

	genericParser := database.NewParser(parser.ParserModePostgres)
	postgresParser := NewParserWithMode(PsqldefParserModePgquery)
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			psqlResult, err := postgresParser.Parse(test.SQL)
			if err != nil {
				t.Fatal(err)
			}

			if !test.CompareWithGenericParser {
				return
			}

			genericResult, err := genericParser.Parse(test.SQL)
			if err != nil {
				t.Fatal(err)
			}

			assert.Equal(t, genericResult, psqlResult)
		})
	}
}

type TestCase struct {
	SQL                      string
	CompareWithGenericParser bool `yaml:"compare_with_generic_parser"`
}

func readTests(file string) (map[string]TestCase, error) {
	buf, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	var tests map[string]TestCase
	dec := yaml.NewDecoder(bytes.NewReader(buf), yaml.DisallowUnknownField())
	err = dec.Decode(&tests)
	if err != nil {
		return nil, err
	}

	return tests, nil
}

// TestParseIndexAsync tests parsing of CREATE INDEX ASYNC without database execution.
// This is a parse-only test since regular PostgreSQL doesn't support ASYNC (Aurora DSQL only).
// The parser uses testing=false to allow fallback to the generic parser.
func TestParseIndexAsync(t *testing.T) {
	// Create parser with testing=false to enable generic parser fallback
	sqlParser := NewParser()

	// Test parsing CREATE INDEX ASYNC
	sql := `CREATE TABLE users (
  id BIGINT NOT NULL PRIMARY KEY,
  name VARCHAR(128) DEFAULT 'konsumer'
);
CREATE INDEX ASYNC username on users (name);`

	// Parse the schema - should not error (will use generic parser fallback)
	statements, err := sqlParser.Parse(sql)
	if err != nil {
		t.Fatalf("failed to parse CREATE INDEX ASYNC: %v", err)
	}

	// Verify we got 2 statements
	if len(statements) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(statements))
	}

	// Verify second statement is CREATE INDEX with Async flag
	indexStmt := statements[1].Statement
	ddl, ok := indexStmt.(*parser.DDL)
	if !ok {
		t.Fatalf("expected DDL statement, got %T", indexStmt)
	}

	if ddl.Action != parser.CreateIndex {
		t.Fatalf("expected CreateIndex action, got %v", ddl.Action)
	}

	if ddl.IndexSpec == nil {
		t.Fatal("expected IndexSpec to be non-nil")
	}

	if !ddl.IndexSpec.Async {
		t.Error("expected Async flag to be true")
	}

	// Verify the generated DDL string contains ASYNC
	generatedDDL := statements[1].DDL
	if !strings.Contains(strings.ToUpper(generatedDDL), "ASYNC") {
		t.Errorf("expected ASYNC in generated DDL, got: %s", generatedDDL)
	}
}

func TestCreateFunctionWithPgquery(t *testing.T) {
	postgresParser := NewParserWithMode(PsqldefParserModePgquery)

	statements, err := postgresParser.Parse(`
    CREATE FUNCTION increment(i integer) RETURNS integer
    LANGUAGE plpgsql
    AS $$
      BEGIN
        RETURN i + 1;
      END;
    $$;
	`)

	if err != nil {
		t.Fatalf("failed to parse CREATE FUNCTION: %v", err)
	}

	if len(statements) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(statements))
	}

	funcStmt := statements[0].Statement
	_, ok := funcStmt.(*parser.Ignore)
	if !ok {
		t.Fatalf("expected Ignore, got %T", funcStmt)
	}
}

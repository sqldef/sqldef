package schema

import (
	"strings"
	"testing"

	"github.com/sqldef/sqldef/v3/database"
	"github.com/sqldef/sqldef/v3/parser"
)

func extractCheckExpr(t *testing.T, sql string) parser.Expr {
	t.Helper()
	stmt, err := parser.ParseDDL(sql, parser.ParserModePostgres)
	if err != nil {
		t.Fatalf("ParseDDL(%q) failed: %v", sql, err)
	}
	ddl, ok := stmt.(*parser.DDL)
	if !ok {
		t.Fatalf("expected *parser.DDL, got %T", stmt)
	}
	if len(ddl.TableSpec.Checks) == 0 {
		t.Fatalf("no CHECK constraints found in %q", sql)
	}
	return ddl.TableSpec.Checks[0].Where.Expr
}

func extractFirstColumnDefaultExpr(t *testing.T, sql string) parser.Expr {
	t.Helper()
	stmt, err := parser.ParseDDL(sql, parser.ParserModePostgres)
	if err != nil {
		t.Fatalf("ParseDDL(%q) failed: %v", sql, err)
	}
	ddl, ok := stmt.(*parser.DDL)
	if !ok {
		t.Fatalf("expected *parser.DDL, got %T", stmt)
	}
	for _, col := range ddl.TableSpec.Columns {
		if col.Type.Default != nil {
			return col.Type.Default.Expression.Expr
		}
	}
	t.Fatalf("no DEFAULT expression found in %q", sql)
	return nil
}

func TestNormalizeCheckExprConcatLowercasesNestedFunctionName(t *testing.T) {
	expr := extractCheckExpr(t, `CREATE TABLE t (id int, CHECK ('A' || UPPER('b') <> ''))`)

	normalized := normalizeCheckExpr(expr, GeneratorModePostgres)

	got := parser.String(normalized)
	if !strings.Contains(got, "upper(") {
		t.Errorf("expected UPPER to be normalized to upper inside ||, got: %s", got)
	}
	if strings.Contains(got, "UPPER(") {
		t.Errorf("expected no UPPER inside ||, got: %s", got)
	}
}

func TestNormalizeExprConcatLowercasesNestedFunctionName(t *testing.T) {
	expr := extractFirstColumnDefaultExpr(t, `CREATE TABLE t (s text NOT NULL DEFAULT ('A' || UPPER('b')))`)

	normalized := normalizeExpr(expr, GeneratorModePostgres)

	got := parser.String(normalized)
	if !strings.Contains(got, "upper(") {
		t.Errorf("expected UPPER to be normalized to upper inside ||, got: %s", got)
	}
	if strings.Contains(got, "UPPER(") {
		t.Errorf("expected no UPPER inside ||, got: %s", got)
	}
}

func TestNormalizeExprPreservingQualifiersConcatRecurses(t *testing.T) {
	expr := extractFirstColumnDefaultExpr(t, `CREATE TABLE t (s text NOT NULL DEFAULT ('A' || UPPER('b')))`)

	normalized := normalizeExprPreservingQualifiers(expr, GeneratorModePostgres)

	got := parser.String(normalized)
	if !strings.Contains(got, "upper(") {
		t.Errorf("expected UPPER to be normalized to upper inside ||, got: %s", got)
	}
	if strings.Contains(got, "UPPER(") {
		t.Errorf("expected no UPPER inside ||, got: %s", got)
	}
}

func TestFormatExprQuoteAwarePreservesQuotedColumnInsideConcat(t *testing.T) {
	expr := extractCheckExpr(t, `CREATE TABLE t ("MyCol" text NOT NULL, CHECK (length("MyCol" || '_x') > 2))`)

	g := &Generator{
		mode:   GeneratorModePostgres,
		config: database.GeneratorConfig{LegacyIgnoreQuotes: false},
	}
	got := g.formatExprQuoteAware(expr)

	if !strings.Contains(got, `"MyCol"`) {
		t.Errorf("expected quoted \"MyCol\" to be preserved inside ||, got: %s", got)
	}
}

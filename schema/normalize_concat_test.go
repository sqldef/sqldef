package schema

import (
	"strings"
	"testing"

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

	g := &Generator{dialect: dialect{mode: GeneratorModePostgres}}
	got := g.formatExprQuoteAware(expr)

	if !strings.Contains(got, `"MyCol"`) {
		t.Errorf("expected quoted \"MyCol\" to be preserved inside ||, got: %s", got)
	}
}

func TestNormalizeTrimFunction(t *testing.T) {
	column := func(name string) parser.SelectExpr {
		return &parser.AliasedExpr{Expr: &parser.ColName{Name: parser.NewIdent(name, false)}}
	}

	tests := []struct {
		name      string
		function  *parser.FuncExpr
		want      string
		wantMatch bool
	}{
		{
			name: "trim",
			function: &parser.FuncExpr{
				Name:  parser.NewIdent("TRIM", false),
				Exprs: parser.SelectExprs{column("value")},
			},
			want:      "trim(value)",
			wantMatch: true,
		},
		{
			name: "qualified btrim",
			function: &parser.FuncExpr{
				Qualifier: parser.NewIdent("pg_catalog", false),
				Name:      parser.NewIdent("btrim", false),
				Exprs:     parser.SelectExprs{column("value")},
			},
			want:      "trim(value)",
			wantMatch: true,
		},
		{
			name: "ltrim",
			function: &parser.FuncExpr{
				Name:  parser.NewIdent("ltrim", false),
				Exprs: parser.SelectExprs{column("value")},
			},
			want:      "trim(leading from value)",
			wantMatch: true,
		},
		{
			name: "rtrim",
			function: &parser.FuncExpr{
				Name:  parser.NewIdent("rtrim", false),
				Exprs: parser.SelectExprs{column("value")},
			},
			want:      "trim(trailing from value)",
			wantMatch: true,
		},
		{
			name: "btrim with trim character",
			function: &parser.FuncExpr{
				Name:  parser.NewIdent("btrim", false),
				Exprs: parser.SelectExprs{column("value"), column("chars")},
			},
			want:      "trim(chars from value)",
			wantMatch: true,
		},
		{
			name: "ltrim with trim character",
			function: &parser.FuncExpr{
				Name:  parser.NewIdent("ltrim", false),
				Exprs: parser.SelectExprs{column("value"), column("chars")},
			},
			want:      "trim(leading chars from value)",
			wantMatch: true,
		},
		{
			name: "rtrim with trim character",
			function: &parser.FuncExpr{
				Name:  parser.NewIdent("rtrim", false),
				Exprs: parser.SelectExprs{column("value"), column("chars")},
			},
			want:      "trim(trailing chars from value)",
			wantMatch: true,
		},
		{
			name: "function from another schema",
			function: &parser.FuncExpr{
				Qualifier: parser.NewIdent("public", false),
				Name:      parser.NewIdent("btrim", false),
				Exprs:     parser.SelectExprs{column("value")},
			},
			wantMatch: false,
		},
		{
			name: "unsupported argument count",
			function: &parser.FuncExpr{
				Name:  parser.NewIdent("trim", false),
				Exprs: parser.SelectExprs{column("a"), column("b"), column("c")},
			},
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := normalizeTrimFunction(tt.function, tt.function.Exprs)
			if ok != tt.wantMatch {
				t.Fatalf("normalizeTrimFunction() matched = %v, want %v", ok, tt.wantMatch)
			}
			if !tt.wantMatch {
				return
			}
			if actual := parser.String(got); actual != tt.want {
				t.Errorf("normalizeTrimFunction() = %q, want %q", actual, tt.want)
			}
			if _, ok := got.(*parser.TrimExpr); !ok {
				t.Errorf("normalizeTrimFunction() returned %T, want *parser.TrimExpr", got)
			}
		})
	}
}

func TestNormalizeTrimExprPreservesDirection(t *testing.T) {
	column := func(name string) parser.Expr {
		return &parser.ColName{Name: parser.NewIdent(name, false)}
	}

	tests := []struct {
		direction string
		want      string
	}{
		{direction: "both", want: "trim(value)"},
		{direction: "leading", want: "trim(leading from value)"},
		{direction: "trailing", want: "trim(trailing from value)"},
	}

	for _, tt := range tests {
		t.Run(tt.direction, func(t *testing.T) {
			expr := &parser.TrimExpr{Direction: tt.direction, String: column("value")}
			got := normalizeCheckExpr(expr, GeneratorModePostgres)
			if actual := parser.String(got); actual != tt.want {
				t.Errorf("normalizeCheckExpr() = %q, want %q", actual, tt.want)
			}
		})
	}
}

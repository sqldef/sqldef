package schema

import (
	"reflect"
	"testing"

	"github.com/sqldef/sqldef/v3/parser"
)

func extractDefaultExpr(t *testing.T, sql string) parser.Expr {
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

func TestExtractFunctionCallNamesThroughConcat(t *testing.T) {
	cases := []struct {
		name           string
		sql            string
		knownFunctions map[string]bool
		want           []string
	}{
		{
			name:           "concat with function call on the right",
			sql:            `CREATE TABLE t (s text NOT NULL DEFAULT ('acc_' || new_id()))`,
			knownFunctions: map[string]bool{"public.new_id": true},
			want:           []string{"public.new_id"},
		},
		{
			name:           "chained concat reaches every function call",
			sql:            `CREATE TABLE t (s text NOT NULL DEFAULT (lower('A') || other_fn() || 'z'))`,
			knownFunctions: map[string]bool{"public.lower": true, "public.other_fn": true},
			want:           []string{"public.lower", "public.other_fn"},
		},
		{
			name:           "concat nested inside another function argument",
			sql:            `CREATE TABLE t (s text NOT NULL DEFAULT length('a' || nested_fn()))`,
			knownFunctions: map[string]bool{"public.nested_fn": true},
			want:           []string{"public.nested_fn"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr := extractDefaultExpr(t, tc.sql)
			got := extractFunctionCallNames(expr, tc.knownFunctions, "public", GeneratorModePostgres, false, 0)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("extractFunctionCallNames = %v, want %v", got, tc.want)
			}
		})
	}
}

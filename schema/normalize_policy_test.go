package schema

import (
	"testing"

	"github.com/sqldef/sqldef/v3/parser"
)

func extractPolicyUsingExpr(t *testing.T, exprSQL string) parser.Expr {
	t.Helper()
	sql := "CREATE POLICY p ON t USING (" + exprSQL + ")"
	stmt, err := parser.ParseDDL(sql, parser.ParserModePostgres)
	if err != nil {
		t.Fatalf("ParseDDL(%q) failed: %v", sql, err)
	}
	ddl, ok := stmt.(*parser.DDL)
	if !ok {
		t.Fatalf("expected *parser.DDL, got %T", stmt)
	}
	if ddl.Policy == nil || ddl.Policy.Using == nil {
		t.Fatalf("no USING expression found in %q", sql)
	}
	return ddl.Policy.Using.Expr
}

func TestStripFuncResultTextCasts(t *testing.T) {
	testCases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "strips text cast on function result",
			input: "current_setting('app.tenant_id', true)::text = tenant_id::text",
			want:  "current_setting('app.tenant_id', true) = tenant_id::text",
		},
		{
			name:  "strips text cast on parenthesized function result",
			input: "(current_setting('app.tenant_id', true))::text = 'x'",
			want:  "current_setting('app.tenant_id', true) = 'x'",
		},
		{
			name:  "strips character varying cast on function result",
			input: "current_user::character varying = 'x'",
			want:  "current_user::character varying = 'x'", // current_user is a keyword, not a function call
		},
		{
			name:  "strips character varying cast on function call",
			input: "lower('X')::character varying = 'x'",
			want:  "lower('X') = 'x'",
		},
		{
			name:  "keeps non-text cast on function result",
			input: "current_setting('app.tenant_id', true)::integer = 1",
			want:  "current_setting('app.tenant_id', true)::integer = 1",
		},
		{
			name:  "keeps text cast on column",
			input: "tenant_id::text = 'x'",
			want:  "tenant_id::text = 'x'",
		},
		{
			name:  "keeps text cast on literal",
			input: "'x'::text = name",
			want:  "'x'::text = name",
		},
		{
			name:  "strips inner text cast under outer non-text cast",
			input: "lower('X')::text::integer = 1",
			want:  "lower('X')::integer = 1",
		},
		{
			name:  "recurses into NOT",
			input: "NOT (lower('X')::text = 'x')",
			want:  "NOT (lower('X') = 'x')",
		},
		{
			name:  "recurses into AND and OR",
			input: "lower('a')::text = 'a' AND (lower('b')::text = 'b' OR tenant_id = 1)",
			want:  "lower('a') = 'a' AND (lower('b') = 'b' OR tenant_id = 1)",
		},
		{
			name:  "recurses into string concatenation",
			input: "lower('a')::text || 'x' = 'ax'",
			want:  "lower('a') || 'x' = 'ax'",
		},
		{
			name:  "recurses into binary operator",
			input: "tenant_id + 1 = 2",
			want:  "tenant_id + 1 = 2",
		},
		{
			name:  "recurses into BETWEEN",
			input: "name BETWEEN lower('a')::text AND lower('z')::text",
			want:  "name BETWEEN lower('a') AND lower('z')",
		},
		{
			name:  "recurses into IS NULL",
			input: "current_setting('app.tenant_id', true)::text IS NULL",
			want:  "current_setting('app.tenant_id', true) IS NULL",
		},
		{
			name:  "recurses into function arguments",
			input: "upper(lower('x')::text) = 'X'",
			want:  "upper(lower('x')) = 'X'",
		},
		{
			name:  "keeps other node types as-is",
			input: "count(*) > 0",
			want:  "count(*) > 0",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			input := extractPolicyUsingExpr(t, tc.input)
			want := extractPolicyUsingExpr(t, tc.want)

			got := parser.String(stripFuncResultTextCasts(input))
			if got != parser.String(want) {
				t.Errorf("stripFuncResultTextCasts(%q) = %q, want %q", tc.input, got, parser.String(want))
			}
		})
	}

	t.Run("nil expression", func(t *testing.T) {
		if got := stripFuncResultTextCasts(nil); got != nil {
			t.Errorf("stripFuncResultTextCasts(nil) = %v, want nil", got)
		}
	})
}

func TestAreSamePolicyExprs(t *testing.T) {
	g := &Generator{mode: GeneratorModePostgres}

	testCases := []struct {
		name  string
		exprA string
		exprB string
		want  bool
	}{
		{
			name:  "equal without stripping",
			exprA: "tenant_id = 1",
			exprB: "tenant_id = 1",
			want:  true,
		},
		{
			// PostgreSQL stores the left form for a policy written in the right form
			name:  "equal after stripping text cast on function result",
			exprA: "(tenant_id)::text = current_setting('app.tenant_id'::text, true)",
			exprB: "tenant_id::text = current_setting('app.tenant_id', true)::text",
			want:  true,
		},
		{
			name:  "different expressions",
			exprA: "tenant_id = 1",
			exprB: "tenant_id = 2",
			want:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			exprA := extractPolicyUsingExpr(t, tc.exprA)
			exprB := extractPolicyUsingExpr(t, tc.exprB)

			if got := g.areSamePolicyExprs(exprA, exprB); got != tc.want {
				t.Errorf("areSamePolicyExprs(%q, %q) = %v, want %v", tc.exprA, tc.exprB, got, tc.want)
			}
		})
	}

	t.Run("no cast stripping fallback for non-postgres mode", func(t *testing.T) {
		mysqlG := &Generator{mode: GeneratorModeMysql}
		exprA := extractPolicyUsingExpr(t, "lower('a')::text = 'a'")
		exprB := extractPolicyUsingExpr(t, "lower('a') = 'a'")
		if mysqlG.areSamePolicyExprs(exprA, exprB) {
			t.Error("expected areSamePolicyExprs to be false for non-postgres mode")
		}
	})
}

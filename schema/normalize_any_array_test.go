package schema

import (
	"testing"

	"github.com/sqldef/sqldef/v3/database"
	"github.com/sqldef/sqldef/v3/parser"
)

func TestNormalizeCheckExprSortsAnyAllArray(t *testing.T) {
	tests := []struct {
		name     string
		sql      string
		expected string
	}{
		{
			name:     "unsorted IN",
			sql:      `CREATE TABLE t (status text, CHECK (status IN ('pending', 'active')))`,
			expected: "status = ANY (ARRAY['active', 'pending'])",
		},
		{
			name:     "unsorted ANY as stored by PostgreSQL",
			sql:      `CREATE TABLE t (status text, CHECK (status = ANY (ARRAY['pending'::text, 'active'::text])))`,
			expected: "status = ANY (ARRAY['active', 'pending'])",
		},
		{
			// Element order is irrelevant for every ANY/ALL operator, not just = and <>.
			name:     "unsorted ALL with a non-equality operator",
			sql:      `CREATE TABLE t (priority int, CHECK (priority >= ALL (ARRAY[2, 1])))`,
			expected: "priority >= ALL (ARRAY[1, 2])",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.String(normalizeCheckExpr(extractCheckExpr(t, tt.sql), GeneratorModePostgres))
			if got != tt.expected {
				t.Errorf("normalizeCheckExpr() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestNormalizeCheckExprNormalizesNotInToAllComparison(t *testing.T) {
	tests := []struct {
		name     string
		sql      string
		expected string
	}{
		{
			name:     "NOT IN",
			sql:      `CREATE TABLE t (status text, CHECK (status NOT IN ('deleted', 'cancelled')))`,
			expected: "status <> ALL (ARRAY['cancelled', 'deleted'])",
		},
		{
			name:     "ALL as stored by PostgreSQL",
			sql:      `CREATE TABLE t (status text, CHECK (status <> ALL (ARRAY['deleted'::text, 'cancelled'::text])))`,
			expected: "status <> ALL (ARRAY['cancelled', 'deleted'])",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.String(normalizeCheckExpr(extractCheckExpr(t, tt.sql), GeneratorModePostgres))
			if got != tt.expected {
				t.Errorf("normalizeCheckExpr() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// PostgreSQL folds IN ('a') into = 'a' but keeps ARRAY[...] for an explicitly written
// ANY/ALL, so both spellings must normalize to the same scalar comparison.
func TestNormalizeSingleElementArrayComparison(t *testing.T) {
	tests := []struct {
		name     string
		sql      string
		expected string
	}{
		{
			name:     "single element IN",
			sql:      `CREATE TABLE t (status text, CHECK (status IN ('pending')))`,
			expected: "status = 'pending'",
		},
		{
			name:     "single element ANY",
			sql:      `CREATE TABLE t (status text, CHECK (status = ANY (ARRAY['pending'::text])))`,
			expected: "status = 'pending'",
		},
		{
			name:     "single element NOT IN",
			sql:      `CREATE TABLE t (status text, CHECK (status NOT IN ('pending')))`,
			expected: "status <> 'pending'",
		},
		{
			name:     "single element ALL",
			sql:      `CREATE TABLE t (status text, CHECK (status <> ALL (ARRAY['pending'::text])))`,
			expected: "status <> 'pending'",
		},
		{
			name:     "multiple elements are left as an array",
			sql:      `CREATE TABLE t (status text, CHECK (status = ANY (ARRAY['active'::text, 'pending'::text])))`,
			expected: "status = ANY (ARRAY['active', 'pending'])",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := extractCheckExpr(t, tt.sql)

			// normalizeCheckExpr covers CHECK constraints, normalizeExpr partial index WHERE clauses.
			if got := parser.String(normalizeCheckExpr(expr, GeneratorModePostgres)); got != tt.expected {
				t.Errorf("normalizeCheckExpr() = %q, want %q", got, tt.expected)
			}
			if got := parser.String(normalizeExpr(expr, GeneratorModePostgres)); got != tt.expected {
				t.Errorf("normalizeExpr() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestNormalizeCheckExprStringQuoteAwareKeepsAnyAll(t *testing.T) {
	tests := []struct {
		name     string
		sql      string
		expected string
	}{
		{
			name:     "IN is converted to ANY",
			sql:      `CREATE TABLE t (status text, CHECK (status IN ('active', 'pending')))`,
			expected: "status = ANY (ARRAY['active', 'pending'])",
		},
		{
			name:     "explicit ANY is preserved",
			sql:      `CREATE TABLE t (status text, CHECK (status = ANY (ARRAY['active', 'pending'])))`,
			expected: "status = ANY (ARRAY['active', 'pending'])",
		},
		{
			name:     "explicit ALL is preserved",
			sql:      `CREATE TABLE t (status text, CHECK (status <> ALL (ARRAY['active', 'pending'])))`,
			expected: "status <> ALL (ARRAY['active', 'pending'])",
		},
		{
			name:     "quoted column name is preserved",
			sql:      `CREATE TABLE t ("Status" text, CHECK ("Status" IN ('active', 'pending')))`,
			expected: `"Status" = ANY (ARRAY['active', 'pending'])`,
		},
		{
			// Generated DDL keeps the authored order even though comparison sorts it,
			// so applying a change does not rewrite an order that carries meaning.
			name:     "authored element order is kept",
			sql:      `CREATE TABLE t (status text, CHECK (status IN ('pending', 'active')))`,
			expected: "status = ANY (ARRAY['pending', 'active'])",
		},
		{
			name:     "single element is not folded",
			sql:      `CREATE TABLE t (status text, CHECK (status IN ('pending')))`,
			expected: "status = ANY (ARRAY['pending'])",
		},
		{
			name:     "NOT IN is converted to ALL",
			sql:      `CREATE TABLE t (status text, CHECK (status NOT IN ('deleted', 'cancelled')))`,
			expected: "status <> ALL (ARRAY['deleted', 'cancelled'])",
		},
		// FIXME: the generic parser rejects a column reference inside ARRAY[...], which
		// PostgreSQL accepts. Until it parses, ARRAY elements are literals as far as
		// formatExprQuoteAware is concerned, so falling back to parser.String for them
		// loses no quoting.
		// {
		// 	name:     "quoted column name inside the array is preserved",
		// 	sql:      `CREATE TABLE t ("Status" text, "Fallback" text, CHECK ("Status" = ANY (ARRAY["Fallback", 'pending'])))`,
		// 	expected: `"Status" = ANY (ARRAY["Fallback", 'pending'])`,
		// },
	}

	g := &Generator{
		mode:   GeneratorModePostgres,
		config: database.GeneratorConfig{LegacyIgnoreQuotes: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := g.normalizeCheckExprString(extractCheckExpr(t, tt.sql))
			if got != tt.expected {
				t.Errorf("normalizeCheckExprString() = %q, want %q", got, tt.expected)
			}
		})
	}
}

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
			name:     "unsorted ALL as stored by PostgreSQL",
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

package schema

import (
	"testing"

	"github.com/sqldef/sqldef/v3/database"
)

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

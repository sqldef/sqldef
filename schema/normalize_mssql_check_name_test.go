package schema

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildMssqlCheckConstraintName(t *testing.T) {
	tests := []struct {
		name     string
		table    string
		column   string
		expected string
	}{
		{
			name:     "short names are kept",
			table:    "users",
			column:   "age",
			expected: "users_age_check",
		},
		{
			name:     "table is truncated when column is at most 28 bytes",
			table:    strings.Repeat("t", 50),
			column:   strings.Repeat("c", 10),
			expected: strings.Repeat("t", 46) + "_" + strings.Repeat("c", 10) + "_check",
		},
		{
			name:     "column is truncated to 28 bytes before table",
			table:    strings.Repeat("t", 30),
			column:   strings.Repeat("c", 40),
			expected: strings.Repeat("t", 28) + "_" + strings.Repeat("c", 28) + "_check",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, buildMssqlCheckConstraintName(tt.table, tt.column).Name)
		})
	}
}

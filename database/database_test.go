package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsSingleLineComment(t *testing.T) {
	tests := []struct {
		name     string
		ddl      string
		expected bool
	}{
		{
			name:     "Single line comment",
			ddl:      "-- comment",
			expected: true,
		},
		{
			name:     "Indented single line comment",
			ddl:      "  -- comment",
			expected: false,
		},
		{
			name:     "Single line comment with spaces",
			ddl:      "-- comment \n ",
			expected: true,
		},
		{
			name:     "Single line comment with SQL",
			ddl:      "-- comment\nCREATE TABLE foo (id int)\n",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, isSingleLineComment(tt.ddl), tt.expected)
		})
	}
}

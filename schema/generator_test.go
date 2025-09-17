package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStringConstantSimple(t *testing.T) {
	assert.Equal(t, StringConstant(""), "''")
	assert.Equal(t, StringConstant("hello world"), "'hello world'")
}

func TestStringConstantContainingSingleQuote(t *testing.T) {
	assert.Equal(t, StringConstant("it's the bee's knees"), "'it''s the bee''s knees'")
	assert.Equal(t, StringConstant("'"), "''''")
	assert.Equal(t, StringConstant("''"), "''''''")
	assert.Equal(t, StringConstant("'example'"), "'''example'''")
}

func TestAreSamePrimaryKeyColumnsMutation(t *testing.T) {
	// Test that areSamePrimaryKeyColumns doesn't mutate the input indexes
	g := &Generator{mode: GeneratorModeMysql}

	// Create two indexes with empty directions
	indexA := Index{
		primary: true,
		columns: []IndexColumn{
			{column: "id", direction: ""},
			{column: "name", direction: ""},
		},
	}

	indexB := Index{
		primary: true,
		columns: []IndexColumn{
			{column: "id", direction: ""},
			{column: "name", direction: ""},
		},
	}

	// Store original direction values to check they weren't mutated
	originalBDirection0 := indexB.columns[0].direction
	originalBDirection1 := indexB.columns[1].direction

	// Call the function which currently mutates indexB
	result := g.areSamePrimaryKeyColumns(indexA, indexB)

	// The function should return true (they are the same)
	assert.True(t, result, "Indexes should be considered the same")

	// BUG: The directions should not have been mutated
	// This will FAIL with the current implementation
	assert.Equal(t, originalBDirection0, indexB.columns[0].direction, "indexB.columns[0].direction was mutated")
	assert.Equal(t, originalBDirection1, indexB.columns[1].direction, "indexB.columns[1].direction was mutated")
}

func TestAreSamePrimaryKeyColumnsWithDifferentDirections(t *testing.T) {
	// Test comparing primary keys with different explicit directions
	g := &Generator{mode: GeneratorModeMysql}

	indexA := Index{
		primary: true,
		columns: []IndexColumn{
			{column: "id", direction: AscScr},
			{column: "name", direction: DescScr},
		},
	}

	indexB := Index{
		primary: true,
		columns: []IndexColumn{
			{column: "id", direction: AscScr},
			{column: "name", direction: AscScr}, // Different direction
		},
	}

	// Store original values
	originalBDirection0 := indexB.columns[0].direction
	originalBDirection1 := indexB.columns[1].direction

	// Should return false due to different directions
	result := g.areSamePrimaryKeyColumns(indexA, indexB)
	assert.False(t, result, "Indexes with different directions should not be the same")

	// Verify no mutation occurred
	assert.Equal(t, originalBDirection0, indexB.columns[0].direction, "indexB.columns[0].direction should not be mutated")
	assert.Equal(t, originalBDirection1, indexB.columns[1].direction, "indexB.columns[1].direction should not be mutated")
}

func TestNormalizeViewDefinition(t *testing.T) {
	tests := []struct {
		name     string
		mode     GeneratorMode
		input    string
		expected string
	}{
		// PostgreSQL specific tests
		{
			name:     "PostgreSQL: normalize table prefix with COLLATE",
			mode:     GeneratorModePostgres,
			input:    `select users.id, (users.name COLLATE "ja-JP-x-icu") as name from users`,
			expected: `select id, (name collate "ja-jp-x-icu") as name from users`,
		},
		{
			name:     "PostgreSQL: normalize multiple table prefixes",
			mode:     GeneratorModePostgres,
			input:    `select users.id, users.name, users.email from users`,
			expected: `select id, name, email from users`,
		},
		{
			name:     "PostgreSQL: normalize with lowercase collate",
			mode:     GeneratorModePostgres,
			input:    `select users.id, (users.name collate "ja-JP-x-icu") as name from users`,
			expected: `select id, (name collate "ja-jp-x-icu") as name from users`,
		},
		{
			name:     "PostgreSQL: normalize spaces",
			mode:     GeneratorModePostgres,
			input:    `select   users.id,    (users.name   COLLATE   "ja-JP-x-icu")   as   name   from   users`,
			expected: `select id, (name collate "ja-jp-x-icu") as name from users`,
		},
		{
			name:     "PostgreSQL: normalize with joins",
			mode:     GeneratorModePostgres,
			input:    `select u.id, (u.name COLLATE "en_US") as name from users u join orders o on u.id = o.user_id`,
			expected: `select id, (name collate "en_us") as name from users u join orders o on id = user_id`,
		},
		{
			name:     "PostgreSQL: preserve column names without prefixes",
			mode:     GeneratorModePostgres,
			input:    `select id, (name COLLATE "ja-JP-x-icu") as name from users`,
			expected: `select id, (name collate "ja-jp-x-icu") as name from users`,
		},
		{
			name:     "PostgreSQL: normalize array syntax",
			mode:     GeneratorModePostgres,
			input:    `select array[1, 2, 3] as nums`,
			expected: `select 1, 2, 3 as nums`,
		},
		// Non-PostgreSQL modes should not normalize
		{
			name:     "MySQL: no normalization",
			mode:     GeneratorModeMysql,
			input:    `SELECT users.id, users.name FROM users`,
			expected: `select users.id, users.name from users`,
		},
		{
			name:     "SQLite3: no normalization",
			mode:     GeneratorModeSQLite3,
			input:    `SELECT users.id, users.name FROM users`,
			expected: `select users.id, users.name from users`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &Generator{mode: tt.mode}
			actual := g.normalizeViewDefinition(tt.input)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

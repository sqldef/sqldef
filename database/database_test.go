package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsCommentedOut(t *testing.T) {
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
		{
			name:     "Fully commented multi-line statement",
			ddl:      "-- Skipped: CREATE FUNCTION f() RETURNS event_trigger AS $$\n-- BEGIN\n-- END;\n-- $$ LANGUAGE plpgsql;",
			expected: true,
		},
		{
			name:     "Partially commented multi-line statement",
			ddl:      "-- Skipped: CREATE FUNCTION f() RETURNS event_trigger AS $$\nBEGIN\nEND;\n$$ LANGUAGE plpgsql;",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, isCommentedOut(tt.ddl), tt.expected)
		})
	}
}

func TestParseGeneratorConfigManagePrivilege(t *testing.T) {
	// No manage: section → nil (feature off).
	config := ParseGeneratorConfigString("managed_roles: [app_user]", GeneratorConfig{})
	assert.Nil(t, config.ManagePrivileges)

	// Rules are parsed with target and drop; drop defaults to false.
	config = ParseGeneratorConfigString("manage: {privilege: [{target: 'readonly_.*'}, {target: 'app_.*', drop: true}]}", GeneratorConfig{})
	if assert.NotNil(t, config.ManagePrivileges) {
		rules := *config.ManagePrivileges
		assert.Equal(t, 2, len(rules))
		assert.Equal(t, "readonly_.*", rules[0].Target)
		assert.False(t, rules[0].Drop)
		assert.Equal(t, "app_.*", rules[1].Target)
		assert.True(t, rules[1].Drop)
	}

	// An empty privilege section is distinct from an absent one: it manages
	// all grantees with REVOKE disabled.
	config = ParseGeneratorConfigString("manage: {privilege: []}", GeneratorConfig{})
	if assert.NotNil(t, config.ManagePrivileges) {
		assert.Equal(t, 0, len(*config.ManagePrivileges))
	}

	// A known but unimplemented manage: key is ignored without affecting
	// the privilege rules.
	config = ParseGeneratorConfigString("manage: {table: [{target: foo}], privilege: [{target: bar}]}", GeneratorConfig{})
	if assert.NotNil(t, config.ManagePrivileges) {
		assert.Equal(t, 1, len(*config.ManagePrivileges))
	}
}

func TestMatchManageObjectRule(t *testing.T) {
	// Empty rules list matches everything with drop disabled.
	rule, ok := MatchManageObjectRule([]ManageObjectRule{}, "any_role")
	assert.True(t, ok)
	assert.False(t, rule.Drop)

	rules := []ManageObjectRule{
		{Target: "readonly_.*", Drop: false},
		{Target: "read.*", Drop: true},
		{Target: "", Drop: true},
	}

	// First match wins even when later rules also match.
	rule, ok = MatchManageObjectRule(rules, "readonly_user")
	assert.True(t, ok)
	assert.False(t, rule.Drop)

	// Target is anchored: a substring match is not enough to hit rule 1.
	rule, ok = MatchManageObjectRule(rules, "app_reader")
	assert.True(t, ok) // falls through to the empty target
	assert.True(t, rule.Drop)

	// An empty target matches everything.
	rule, ok = MatchManageObjectRule(rules[2:], "whatever")
	assert.True(t, ok)
	assert.True(t, rule.Drop)

	// No rule matches → not managed.
	_, ok = MatchManageObjectRule(rules[:2], "unrelated")
	assert.False(t, ok)
}

func TestParseGeneratorConfigManageFunction(t *testing.T) {
	// No manage: section → nil (feature off).
	config := ParseGeneratorConfigString("", GeneratorConfig{})
	assert.Nil(t, config.ManageFunctions)

	// Rules are parsed with target and drop; drop defaults to false.
	config = ParseGeneratorConfigString("manage: {function: [{target: 'app_.*'}, {target: 'tmp_.*', drop: true}]}", GeneratorConfig{})
	if assert.NotNil(t, config.ManageFunctions) {
		rules := *config.ManageFunctions
		assert.Equal(t, 2, len(rules))
		assert.Equal(t, "app_.*", rules[0].Target)
		assert.False(t, rules[0].Drop)
		assert.Equal(t, "tmp_.*", rules[1].Target)
		assert.True(t, rules[1].Drop)
	}

	// An empty function section manages all functions with drop disabled.
	config = ParseGeneratorConfigString("manage: {function: []}", GeneratorConfig{})
	if assert.NotNil(t, config.ManageFunctions) {
		assert.Equal(t, 0, len(*config.ManageFunctions))
	}

	// manage.function and manage.privilege coexist independently.
	config = ParseGeneratorConfigString("manage: {function: [{target: f}], privilege: [{target: p}]}", GeneratorConfig{})
	assert.NotNil(t, config.ManageFunctions)
	assert.NotNil(t, config.ManagePrivileges)
}

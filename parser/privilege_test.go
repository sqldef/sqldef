package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPrivilegeString(t *testing.T) {
	tests := []struct {
		name     string
		cols     []Ident
		expected string
	}{
		{"select", nil, "SELECT"},
		{"ALL", nil, "ALL"},
		// Column names are sorted, so declaration order does not matter
		{"select", []Ident{{Name: "secret"}, {Name: "id"}}, "SELECT (id, secret)"},
		{"select", []Ident{{Name: "id"}, {Name: "secret"}}, "SELECT (id, secret)"},
		// A quoted simple lowercase name is identical to its unquoted form
		{"update", []Ident{{Name: "id", Quoted: true}}, "UPDATE (id)"},
		// Names that cannot appear unquoted keep their quotes, doubled inside
		{"update", []Ident{{Name: "Key"}}, `UPDATE ("Key")`},
		{"update", []Ident{{Name: `Odd, "Name`}}, `UPDATE ("Odd, ""Name")`},
		{"update", []Ident{{Name: "a b"}}, `UPDATE ("a b")`},
	}

	for _, test := range tests {
		assert.Equal(t, test.expected, NewPrivilege(test.name, test.cols).String())
	}
}

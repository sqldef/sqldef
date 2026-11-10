package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSameForComparison(t *testing.T) {
	tests := []struct {
		a, b string
		same bool
	}{
		{"nextval('T_seq'::regclass)", "nextval('t_seq'::regclass)", true},
		{"id <> 'A0EEBC99-9C0B-4EF8-BB6D-6BB9BD380A11'", "id <> 'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11'::uuid", true},
		{"a = 'Active'", "a = 'active'::text", false},
		{"a = 'Active'::text", "a = 'active'::text", false},
		{"(data ->> 'Count')::integer > 0", "(data ->> 'count')::integer > 0", true},
		{"(data ->> 'Count'::text)::integer > 0", "(data ->> 'count'::text)::integer > 0", false},
		{"LOWER(\"MyCol\")", "lower(\"MyCol\")", true},
		{"lower(\"MyCol\")", "lower(mycol)", false},
	}
	for _, tt := range tests {
		a, err := parseExprForTest(tt.a)
		assert.NoError(t, err)
		b, err := parseExprForTest(tt.b)
		assert.NoError(t, err)
		assert.Equal(t, tt.same, SameForComparison(a, b, false, false), "%s vs %s", tt.a, tt.b)
	}
}

func parseExprForTest(expr string) (Expr, error) {
	stmt, err := ParseDDL("CREATE TABLE t (a text, CONSTRAINT c CHECK ("+expr+"))", ParserModePostgres)
	if err != nil {
		return nil, err
	}
	return stmt.(*DDL).TableSpec.Checks[0].Where.Expr, nil
}

package schema

import (
	"testing"

	"github.com/sqldef/sqldef/v3/parser"
)

// The pgquery fallback yields a cast target whose type name carries the
// timezone (e.g. "timestamptz"). normalizeTypeName folds those aliases down to
// "timestamp"/"time" on the assumption that a separate Timezone flag preserves
// the modifier, which is true for ColumnType but not for ConvertType. Normalize
// must recover the modifier so the cast target keeps its type.
func TestNormalizeExprRecoversTimezoneFromCastTypeAlias(t *testing.T) {
	base := extractFirstColumnDefaultExpr(t, `CREATE TABLE t (a timestamp DEFAULT now()::timestamp(0))`)
	cast, ok := base.(*parser.CastExpr)
	if !ok {
		t.Fatalf("expected *parser.CastExpr, got %T", base)
	}

	cases := []struct {
		typeName string
		length   *parser.SQLVal
		want     string
	}{
		{"timestamptz", parser.NewIntVal("0"), "now()::timestamp(0) with time zone"},
		{"timestamptz", nil, "now()::timestamp with time zone"},
		{"timetz", nil, "now()::time with time zone"},
		{"timestamp with time zone", nil, "now()::timestamp with time zone"},
		{"time with time zone", nil, "now()::time with time zone"},
	}
	for _, tc := range cases {
		expr := &parser.CastExpr{
			Expr: cast.Expr,
			Type: &parser.ConvertType{Type: tc.typeName, Length: tc.length},
		}
		got := parser.String(normalizeExpr(expr, GeneratorModePostgres))
		if got != tc.want {
			t.Errorf("normalizeExpr(::%s) = %q, want %q", tc.typeName, got, tc.want)
		}
	}
}

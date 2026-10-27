package schema

import (
	"testing"

	"github.com/sqldef/sqldef/v3/parser"
	"github.com/stretchr/testify/assert"
)

func col(name string) *parser.ColName {
	return &parser.ColName{Name: parser.NewIdent(name, false)}
}

// pgquery does not keep parentheses, and the CHECK normalizer unwraps them.
// The printer has to put back the ones PostgreSQL's precedence would rebind.
// Quote-aware mode uses a different printer, so both modes have to be checked.
func TestCheckExprBooleanPrecedence(t *testing.T) {
	equality := &parser.ComparisonExpr{
		Operator: "=",
		Left:     &parser.IsExpr{Operator: parser.IsNullStr, Expr: col("archived_at")},
		Right:    &parser.IsExpr{Operator: parser.IsNullStr, Expr: col("archived_by_email")},
	}
	orInsideAnd := &parser.AndExpr{
		Left: &parser.ComparisonExpr{Operator: "=", Left: col("growth_status"), Right: parser.NewBoolVal(true)},
		Right: &parser.OrExpr{
			Left:  &parser.IsExpr{Operator: parser.IsNullStr, Expr: col("growth_status_reason")},
			Right: &parser.ComparisonExpr{Operator: "=", Left: col("growth_status_reason"), Right: parser.NewStrVal("WAITLIST")},
		},
	}

	tests := []struct {
		name               string
		legacyIgnoreQuotes bool
		equality           string
		orInsideAnd        string
	}{
		{
			name:               "legacy",
			legacyIgnoreQuotes: true,
			equality:           "(archived_at is null) = (archived_by_email is null)",
			orInsideAnd:        "growth_status = true and (growth_status_reason is null or growth_status_reason = 'WAITLIST')",
		},
		{
			name:               "quote-aware",
			legacyIgnoreQuotes: false,
			equality:           "(archived_at is null) = (archived_by_email is null)",
			orInsideAnd:        "growth_status = true AND (growth_status_reason is null OR growth_status_reason = 'WAITLIST')",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := dialect{mode: GeneratorModePostgres, legacyIgnoreQuotes: tt.legacyIgnoreQuotes}
			assert.Equal(t, tt.equality, d.normalizeCheckExprString(equality))
			assert.Equal(t, tt.orInsideAnd, d.normalizeCheckExprString(orInsideAnd))
		})
	}
}

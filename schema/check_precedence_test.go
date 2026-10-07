package schema

import (
	"testing"

	"github.com/sqldef/sqldef/v3/database"
	"github.com/sqldef/sqldef/v3/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func col(name string) *parser.ColName {
	return &parser.ColName{Name: parser.NewIdent(name, false)}
}

// pgquery does not keep parentheses, and the CHECK normalizer unwraps them.
// The printer has to put back the ones PostgreSQL's precedence would rebind.
func TestCheckExprBooleanPrecedence(t *testing.T) {
	d := dialect{mode: GeneratorModePostgres, legacyIgnoreQuotes: true}

	equality := &parser.ComparisonExpr{
		Operator: "=",
		Left:     &parser.IsExpr{Operator: parser.IsNullStr, Expr: col("archived_at")},
		Right:    &parser.IsExpr{Operator: parser.IsNullStr, Expr: col("archived_by_email")},
	}
	assert.Equal(t,
		"(archived_at is null) = (archived_by_email is null)",
		d.normalizeCheckExprString(equality),
	)

	orInsideAnd := &parser.AndExpr{
		Left: &parser.ComparisonExpr{Operator: "=", Left: col("growth_status"), Right: parser.NewBoolVal(true)},
		Right: &parser.OrExpr{
			Left:  &parser.IsExpr{Operator: parser.IsNullStr, Expr: col("growth_status_reason")},
			Right: &parser.ComparisonExpr{Operator: "=", Left: col("growth_status_reason"), Right: parser.NewStrVal("WAITLIST")},
		},
	}
	assert.Equal(t,
		"growth_status = true and (growth_status_reason is null or growth_status_reason = 'WAITLIST')",
		d.normalizeCheckExprString(orInsideAnd),
	)
}

func TestParsedCheckKeepsParentheses(t *testing.T) {
	ddls, err := GenerateIdempotentDDLs(
		GeneratorModePostgres,
		database.NewParser(parser.ParserModePostgres),
		`CREATE TABLE tickets (
			archived_at timestamp,
			archived_by_email text,
			CONSTRAINT both_or_neither CHECK ((archived_at IS NULL) = (archived_by_email IS NULL))
		);
		CREATE TABLE permitting_ahjs (
			growth_status boolean,
			growth_status_reason text,
			CONSTRAINT growth_status_reason_valid CHECK (
				(growth_status = TRUE AND (growth_status_reason IS NULL OR growth_status_reason = 'WAITLIST'))
				OR (growth_status = FALSE AND growth_status_reason IS NULL)
			)
		);`,
		`CREATE TABLE tickets (
			archived_at timestamp,
			archived_by_email text
		);
		CREATE TABLE permitting_ahjs (
			growth_status boolean,
			growth_status_reason text
		);`,
		database.GeneratorConfig{EnableDrop: true, LegacyIgnoreQuotes: true},
		"public",
	)
	require.NoError(t, err)
	joined := ""
	for _, ddl := range ddls {
		joined += ddl + "\n"
	}
	assert.Contains(t, joined, "(archived_at is null) = (archived_by_email is null)")
	assert.Contains(t, joined, "(growth_status_reason is null or growth_status_reason = 'WAITLIST')")
	assert.NotContains(t, joined, "growth_status = true and growth_status_reason is null or growth_status_reason = 'WAITLIST'")
}

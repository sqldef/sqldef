package parser

import (
	"fmt"
	"strings"
)

// ParseSelectStatement parses a SELECT statement string into a SelectStatement AST.
// This is useful for parsing view definitions from PostgreSQL output.
func ParseSelectStatement(sql string, mode ParserMode) (SelectStatement, error) {
	// Trim whitespace and ensure we have a SELECT
	sql = strings.TrimSpace(sql)
	if sql == "" {
		return nil, fmt.Errorf("empty SELECT statement")
	}

	// Wrap the SELECT in a CREATE VIEW to make it parseable by our DDL parser
	// Use a temporary view name that won't conflict
	wrappedDDL := fmt.Sprintf("CREATE VIEW __tmp_parse_view__ AS %s", sql)

	// Parse as DDL
	stmt, err := ParseDDL(wrappedDDL, mode)
	if err != nil {
		return nil, fmt.Errorf("failed to parse SELECT statement: %w", err)
	}

	// Extract the SelectStatement from the View definition
	ddl, ok := stmt.(*DDL)
	if !ok || ddl.Action != CreateView || ddl.View == nil {
		return nil, fmt.Errorf("parsed statement is not a CREATE VIEW")
	}

	return ddl.View.Definition, nil
}

// ParseExpression parses a single expression string into an Expr AST.
// This is useful for parsing index expressions, CHECK constraints, etc.
func ParseExpression(exprStr string, mode ParserMode) (Expr, error) {
	// Wrap the expression in a simple SELECT to make it parseable
	selectSQL := fmt.Sprintf("SELECT %s", exprStr)

	selectStmt, err := ParseSelectStatement(selectSQL, mode)
	if err != nil {
		return nil, fmt.Errorf("failed to parse expression: %w", err)
	}

	// Extract the expression from the SELECT
	sel, ok := selectStmt.(*Select)
	if !ok || len(sel.SelectExprs) == 0 {
		return nil, fmt.Errorf("parsed statement is not a simple SELECT")
	}

	// Get the first select expression
	aliasedExpr, ok := sel.SelectExprs[0].(*AliasedExpr)
	if !ok {
		return nil, fmt.Errorf("select expression is not an AliasedExpr")
	}

	return aliasedExpr.Expr, nil
}

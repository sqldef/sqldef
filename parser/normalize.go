package parser

import (
	"log"
	"strings"
)

// NormalizeSelectStatement normalizes a SelectStatement by removing PostgreSQL-specific
// artifacts and formatting differences that don't affect semantic meaning.
func NormalizeSelectStatement(stmt SelectStatement) SelectStatement {
	if stmt == nil {
		return nil
	}

	switch s := stmt.(type) {
	case *Select:
		return normalizeSelect(s)
	case *Union:
		return normalizeUnion(s)
	case *ParenSelect:
		return normalizeParenSelect(s)
	default:
		return stmt
	}
}

func normalizeSelect(sel *Select) *Select {
	if sel == nil {
		return nil
	}

	// Create a copy to avoid modifying the original
	normalized := &Select{
		Cache:       sel.Cache,
		Comments:    nil, // Clear comments for comparison - PostgreSQL doesn't preserve them
		Distinct:    sel.Distinct,
		Hints:       sel.Hints,
		SelectExprs: normalizeSelectExprs(sel.SelectExprs),
		From:        normalizeTableExprs(sel.From),
		Where:       normalizeWhere(sel.Where),
		GroupBy:     normalizeGroupBy(sel.GroupBy),
		Having:      normalizeWhere(sel.Having),
		OrderBy:     normalizeOrderBy(sel.OrderBy),
		Limit:       sel.Limit,
		Lock:        sel.Lock,
	}

	return normalized
}

func normalizeUnion(u *Union) *Union {
	if u == nil {
		return nil
	}

	return &Union{
		Type:        u.Type,
		Left:        NormalizeSelectStatement(u.Left),
		Right:       NormalizeSelectStatement(u.Right),
		OrderBy:     normalizeOrderBy(u.OrderBy),
		Limit:       u.Limit,
		Lock:        u.Lock,
	}
}

func normalizeParenSelect(ps *ParenSelect) *ParenSelect {
	if ps == nil {
		return nil
	}

	return &ParenSelect{
		Select: NormalizeSelectStatement(ps.Select),
	}
}

func normalizeSelectExprs(exprs SelectExprs) SelectExprs {
	if exprs == nil {
		return nil
	}

	result := make(SelectExprs, len(exprs))
	for i, expr := range exprs {
		result[i] = normalizeSelectExpr(expr)
	}
	return result
}

func normalizeSelectExpr(expr SelectExpr) SelectExpr {
	switch e := expr.(type) {
	case *StarExpr:
		return e
	case *AliasedExpr:
		return &AliasedExpr{
			Expr: NormalizeExpr(e.Expr),
			As:   e.As,
		}
	default:
		return expr
	}
}

func normalizeTableExprs(exprs TableExprs) TableExprs {
	if exprs == nil {
		return nil
	}

	result := make(TableExprs, len(exprs))
	for i, expr := range exprs {
		result[i] = normalizeTableExpr(expr)
	}
	return result
}

func normalizeTableExpr(expr TableExpr) TableExpr {
	switch e := expr.(type) {
	case *AliasedTableExpr:
		return &AliasedTableExpr{
			Expr: normalizeSimpleTableExpr(e.Expr),
			As:   e.As,
		}
	case *ParenTableExpr:
		return &ParenTableExpr{
			Exprs: normalizeTableExprs(e.Exprs),
		}
	case *JoinTableExpr:
		return &JoinTableExpr{
			LeftExpr:  normalizeTableExpr(e.LeftExpr),
			Join:      e.Join,
			RightExpr: normalizeTableExpr(e.RightExpr),
			Condition: JoinCondition{
				On:    NormalizeExpr(e.Condition.On),
				Using: e.Condition.Using,
			},
		}
	default:
		return expr
	}
}

func normalizeSimpleTableExpr(expr SimpleTableExpr) SimpleTableExpr {
	// For simple table expressions, we don't need to normalize much
	return expr
}

func normalizeWhere(where *Where) *Where {
	if where == nil {
		return nil
	}

	return &Where{
		Type: where.Type,
		Expr: NormalizeExpr(where.Expr),
	}
}

func normalizeGroupBy(groupBy GroupBy) GroupBy {
	if groupBy == nil {
		return nil
	}

	result := make(GroupBy, len(groupBy))
	for i, expr := range groupBy {
		result[i] = NormalizeExpr(expr)
	}
	return result
}

func normalizeOrderBy(orderBy OrderBy) OrderBy {
	if orderBy == nil {
		return nil
	}

	result := make(OrderBy, len(orderBy))
	for i, order := range orderBy {
		result[i] = &Order{
			Expr:      NormalizeExpr(order.Expr),
			Direction: order.Direction,
		}
	}
	return result
}

// NormalizeExpr normalizes an expression by removing PostgreSQL-specific artifacts
func NormalizeExpr(expr Expr) Expr {
	if expr == nil {
		return nil
	}

	switch e := expr.(type) {
	case *AndExpr:
		return &AndExpr{
			Left:  NormalizeExpr(e.Left),
			Right: NormalizeExpr(e.Right),
		}

	case *OrExpr:
		return &OrExpr{
			Left:  NormalizeExpr(e.Left),
			Right: NormalizeExpr(e.Right),
		}

	case *NotExpr:
		return &NotExpr{
			Expr: NormalizeExpr(e.Expr),
		}

	case *ParenExpr:
		// Recursively normalize the inner expression
		inner := NormalizeExpr(e.Expr)

		// Remove redundant parentheses around simple expressions
		// Keep parentheses for complex expressions where they matter
		if isSimpleExpr(inner) {
			return inner
		}

		return &ParenExpr{Expr: inner}

	case *ComparisonExpr:
		// Normalize IN (val1, val2, ...) to = ANY (ARRAY[val1, val2, ...])
		// Normalize NOT IN (val1, val2, ...) to != ALL (ARRAY[val1, val2, ...])
		// This matches PostgreSQL's internal representation
		if strings.ToLower(e.Operator) == "in" && !e.All && !e.Any {
			// Check if the right side is a ValTuple (list of values)
			if valTuple, ok := e.Right.(ValTuple); ok {
				// Convert ValTuple to ArrayElements
				elements := make(ArrayElements, len(valTuple))
				for i, val := range valTuple {
					// Cast to ArrayElement (all Expr types that can be in ValTuple implement ArrayElement)
					elements[i] = val.(ArrayElement)
				}
				arrayConstructor := &ArrayConstructor{
					Elements: elements,
				}

				return &ComparisonExpr{
					Left:     NormalizeExpr(e.Left),
					Operator: "=",
					Right:    NormalizeExpr(arrayConstructor),
					Escape:   NormalizeExpr(e.Escape),
					All:      false,
					Any:      true,
				}
			}
		}

		if strings.ToLower(e.Operator) == "not in" && !e.All && !e.Any {
			// Check if the right side is a ValTuple (list of values)
			if valTuple, ok := e.Right.(ValTuple); ok {
				// Convert ValTuple to ArrayElements
				elements := make(ArrayElements, len(valTuple))
				for i, val := range valTuple {
					// Cast to ArrayElement (all Expr types that can be in ValTuple implement ArrayElement)
					elements[i] = val.(ArrayElement)
				}
				arrayConstructor := &ArrayConstructor{
					Elements: elements,
				}

				return &ComparisonExpr{
					Left:     NormalizeExpr(e.Left),
					Operator: "!=",
					Right:    NormalizeExpr(arrayConstructor),
					Escape:   NormalizeExpr(e.Escape),
					All:      true,
					Any:      false,
				}
			}
		}

		return &ComparisonExpr{
			Left:     NormalizeExpr(e.Left),
			Operator: normalizeOperator(e.Operator),
			Right:    NormalizeExpr(e.Right),
			Escape:   NormalizeExpr(e.Escape),
			All:      e.All,
			Any:      e.Any,
		}

	case *IsExpr:
		return &IsExpr{
			Operator: e.Operator,
			Expr:     NormalizeExpr(e.Expr),
		}

	case *ExistsExpr:
		return &ExistsExpr{
			Subquery: normalizeSubquery(e.Subquery),
		}

	case *BinaryExpr:
		return &BinaryExpr{
			Left:     NormalizeExpr(e.Left),
			Operator: e.Operator,
			Right:    NormalizeExpr(e.Right),
		}

	case *UnaryExpr:
		return &UnaryExpr{
			Operator: e.Operator,
			Expr:     NormalizeExpr(e.Expr),
		}

	case *IntervalExpr:
		return &IntervalExpr{
			Expr: NormalizeExpr(e.Expr),
			Unit: e.Unit,
		}

	case *CollateExpr:
		return &CollateExpr{
			Expr:    NormalizeExpr(e.Expr),
			Charset: e.Charset,
		}

	case *FuncExpr:
		return normalizeFuncExpr(e)

	case *FuncCallExpr:
		return normalizeFuncCallExpr(e)

	case *GroupConcatExpr:
		return normalizeGroupConcatExpr(e)

	case *CaseExpr:
		return normalizeCaseExpr(e)

	case *CastExpr:
		return normalizeCastExpr(e)

	case *ConvertExpr:
		return normalizeConvertExpr(e)

	case *SubstrExpr:
		return normalizeSubstrExpr(e)

	case *ColName:
		return normalizeColName(e)

	case *SQLVal:
		return normalizeSQLVal(e)

	case *NullVal:
		return e

	case BoolVal:
		return e

	case ListArg:
		return e

	case *Subquery:
		return normalizeSubquery(e)

	case ValTuple:
		return normalizeValTuple(e)

	default:
		// For unknown types, return as-is
		return expr
	}
}

func normalizeFuncExpr(f *FuncExpr) *FuncExpr {
	if f == nil {
		return nil
	}

	normalizedExprs := make(SelectExprs, len(f.Exprs))
	for i, expr := range f.Exprs {
		normalizedExprs[i] = normalizeSelectExpr(expr)
	}

	// Normalize function name to lowercase (PostgreSQL normalizes all function names to lowercase)
	funcName := strings.ToLower(String(f.Name))

	return &FuncExpr{
		Name:     NewColIdent(funcName),
		Distinct: f.Distinct,
		Exprs:    normalizedExprs,
	}
}

func normalizeFuncCallExpr(f *FuncCallExpr) *FuncCallExpr {
	if f == nil {
		return nil
	}

	// PostgreSQL converts variadic function arguments to ARRAY form
	// e.g., jsonb_extract_path_text(x, 'a', 'b') becomes jsonb_extract_path_text(x, ARRAY['a', 'b'])
	// We need to normalize these back to the individual argument form for comparison
	normalizedExprs := make(Exprs, 0, len(f.Exprs))
	for i, expr := range f.Exprs {
		// Check if this is an ARRAY[...] expression that should be expanded
		if i > 0 {  // Skip first argument (the JSON column)
			if expanded := tryExpandArrayLiteral(expr); expanded != nil {
				normalizedExprs = append(normalizedExprs, expanded...)
				continue
			}
		}
		normalizedExprs = append(normalizedExprs, NormalizeExpr(expr))
	}

	// Normalize function name to lowercase (PostgreSQL normalizes all function names to lowercase)
	funcName := strings.ToLower(String(f.Name))

	return &FuncCallExpr{
		Name:  NewColIdent(funcName),
		Exprs: normalizedExprs,
	}
}

func normalizeGroupConcatExpr(g *GroupConcatExpr) *GroupConcatExpr {
	if g == nil {
		return nil
	}

	normalizedExprs := make(SelectExprs, len(g.Exprs))
	for i, expr := range g.Exprs {
		normalizedExprs[i] = normalizeSelectExpr(expr)
	}

	return &GroupConcatExpr{
		Distinct:  g.Distinct,
		Exprs:     normalizedExprs,
		OrderBy:   normalizeOrderBy(g.OrderBy),
		Separator: g.Separator,
	}
}

func normalizeCaseExpr(c *CaseExpr) *CaseExpr {
	if c == nil {
		return nil
	}

	normalizedWhens := make([]*When, len(c.Whens))
	for i, when := range c.Whens {
		normalizedWhens[i] = &When{
			Cond: NormalizeExpr(when.Cond),
			Val:  NormalizeExpr(when.Val),
		}
	}

	// PostgreSQL adds "ELSE NULL" to CASE expressions
	// We remove it if the else clause is just NULL
	var normalizedElse Expr
	if c.Else != nil {
		if _, isNull := c.Else.(*NullVal); !isNull {
			normalizedElse = NormalizeExpr(c.Else)
		}
		// If it's NULL, leave normalizedElse as nil
	}

	return &CaseExpr{
		Expr:  NormalizeExpr(c.Expr),
		Whens: normalizedWhens,
		Else:  normalizedElse,
	}
}

func normalizeCastExpr(c *CastExpr) Expr {
	if c == nil {
		return nil
	}

	normalizedExpr := NormalizeExpr(c.Expr)

	// Check if this is a redundant cast that PostgreSQL adds
	// e.g., 'text'::text, 123::integer, etc.
	if isRedundantCast(normalizedExpr, c.Type) {
		// Return just the expression, unwrapping the redundant cast
		// This handles cases like 'x'::text -> 'x' and 123::integer -> 123
		return normalizedExpr
	}

	// Remove intermediate casts like ::double precision, ::real
	// PostgreSQL often adds these: ((x)::bigint)::double precision
	// We want to simplify to just the essential cast
	typeStr := strings.ToLower(String(c.Type))
	if strings.Contains(typeStr, "double precision") || strings.Contains(typeStr, "real") {
		// If the inner expression is also a cast, skip intermediate casts
		if innerCast, ok := normalizedExpr.(*CastExpr); ok {
			// Return just the inner cast, removing the outer double precision/real cast
			return innerCast
		}
	}

	// Also handle redundant nested parentheses in casts
	// PostgreSQL creates: ((expr)::type1)::type2
	// We want: expr::type2
	if parenExpr, ok := normalizedExpr.(*ParenExpr); ok {
		if innerCast, ok := parenExpr.Expr.(*CastExpr); ok {
			// Unwrap: (expr::type1)::type2 -> expr::type2
			return &CastExpr{
				Expr: innerCast.Expr,
				Type: c.Type,
			}
		}
	}

	return &CastExpr{
		Expr: normalizedExpr,
		Type: c.Type,
	}
}

func normalizeConvertExpr(c *ConvertExpr) *ConvertExpr {
	if c == nil {
		return nil
	}

	return &ConvertExpr{
		Expr: NormalizeExpr(c.Expr),
		Type: c.Type,
	}
}

func normalizeSubstrExpr(s *SubstrExpr) *SubstrExpr {
	if s == nil {
		return nil
	}

	return &SubstrExpr{
		Name: normalizeSelectExpr(s.Name),
		From: NormalizeExpr(s.From),
		To:   NormalizeExpr(s.To),
	}
}

func normalizeColName(c *ColName) *ColName {
	if c == nil {
		return nil
	}

	// Normalize column references by removing table qualifiers
	// PostgreSQL removes table qualifiers when they're unambiguous
	// For comparison purposes, we strip the qualifier to match PostgreSQL's behavior
	return &ColName{
		Metadata:  c.Metadata,
		Name:      c.Name,
		Qualifier: TableName{}, // Remove table qualifier for comparison
	}
}

func normalizeSQLVal(v *SQLVal) *SQLVal {
	if v == nil {
		return nil
	}

	// No normalization needed for SQL values
	return v
}

func normalizeSubquery(s *Subquery) *Subquery {
	if s == nil {
		return nil
	}

	return &Subquery{
		Select: NormalizeSelectStatement(s.Select),
	}
}

func normalizeValTuple(vt ValTuple) ValTuple {
	if vt == nil {
		return nil
	}

	result := make(ValTuple, len(vt))
	for i, expr := range vt {
		result[i] = NormalizeExpr(expr)
	}
	return result
}

// Helper functions

// tryExpandArrayLiteral checks if the expression is an ARRAY[...] literal from PostgreSQL's
// variadic function transformation and returns the individual elements if so.
// Returns nil if this is not an expandable array literal.
func tryExpandArrayLiteral(expr Expr) Exprs {
	// Check if this is a FuncExpr with name "array" (ARRAY[...] syntax)
	funcExpr, ok := expr.(*FuncExpr)
	if !ok {
		log.Printf("DEBUG tryExpandArrayLiteral: not a FuncExpr, type=%T", expr)
		return nil
	}

	// Check if function name is "array"
	funcName := String(funcExpr.Name)
	log.Printf("DEBUG tryExpandArrayLiteral: funcName=%s", funcName)
	if strings.ToLower(funcName) != "array" {
		return nil
	}

	// Extract the elements from the ARRAY[...] and return them as individual expressions
	result := make(Exprs, 0, len(funcExpr.Exprs))
	for _, selectExpr := range funcExpr.Exprs {
		if aliased, ok := selectExpr.(*AliasedExpr); ok {
			result = append(result, aliased.Expr)
		}
	}

	log.Printf("DEBUG tryExpandArrayLiteral: expanded %d elements", len(result))
	return result
}

func normalizeOperator(op string) string {
	// Normalize PostgreSQL internal operators to standard SQL
	switch op {
	case "~~":
		return "like"
	case "!~~":
		return "not like"
	case "~~*":
		return "ilike"
	case "!~~*":
		return "not ilike"
	default:
		return strings.ToLower(op)
	}
}

func isSimpleExpr(expr Expr) bool {
	// Determine if an expression is simple enough that parentheses are redundant
	switch expr.(type) {
	case *ColName, *SQLVal, *NullVal, BoolVal, ListArg:
		return true
	case *FuncExpr, *FuncCallExpr:
		// Function calls don't need extra parens
		return true
	case *ComparisonExpr, *IsExpr:
		// Comparison expressions (=, !=, <, >, etc.) and IS expressions don't need parens
		return true
	case *NotExpr:
		// NOT expressions don't need parens in WHERE clauses
		return true
	default:
		return false
	}
}

func isRedundantCast(expr Expr, castType *ConvertType) bool {
	// Check if this is a cast that PostgreSQL adds but doesn't change the type
	// e.g., 'text'::text, 123::integer

	switch e := expr.(type) {
	case *SQLVal:
		typeStr := strings.ToLower(String(castType))

		// String literals with text/varchar casts
		if e.Type == StrVal {
			if strings.Contains(typeStr, "text") ||
				strings.Contains(typeStr, "varchar") ||
				strings.Contains(typeStr, "character varying") {
				return true
			}
			// String literals with date/timestamp casts are redundant
			// PostgreSQL accepts date literals as strings, so '2022-01-01'::date can be simplified to '2022-01-01'
			if strings.Contains(typeStr, "date") ||
				strings.Contains(typeStr, "timestamp") ||
				strings.Contains(typeStr, "time") {
				return true
			}
		}

		// Numeric literals with integer/bigint/numeric casts
		if e.Type == IntVal {
			if strings.Contains(typeStr, "integer") ||
				strings.Contains(typeStr, "bigint") ||
				strings.Contains(typeStr, "smallint") ||
				strings.Contains(typeStr, "numeric") {
				return true
			}
		}

		// Float literals with numeric/real/double casts
		if e.Type == FloatVal {
			if strings.Contains(typeStr, "numeric") ||
				strings.Contains(typeStr, "real") ||
				strings.Contains(typeStr, "double") {
				return true
			}
		}
	}

	return false
}

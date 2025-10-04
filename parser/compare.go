package parser

import (
	"bytes"
	"log"
	"reflect"
)

// CompareSelectStatements compares two SelectStatements after normalizing them.
// Returns true if they are semantically equivalent.
func CompareSelectStatements(a, b SelectStatement) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	// Normalize both statements first
	normA := NormalizeSelectStatement(a)
	normB := NormalizeSelectStatement(b)

	// Debug output
	log.Printf("DEBUG CompareSelectStatements:")
	log.Printf("  Normalized A: %s", String(normA))
	log.Printf("  Normalized B: %s", String(normB))

	// Compare the normalized statements
	result := compareSelectStatement(normA, normB)
	log.Printf("  Result: %v", result)
	return result
}

func compareSelectStatement(a, b SelectStatement) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	// Check type equality
	switch sa := a.(type) {
	case *Select:
		sb, ok := b.(*Select)
		if !ok {
			return false
		}
		return compareSelect(sa, sb)

	case *Union:
		ub, ok := b.(*Union)
		if !ok {
			return false
		}
		return compareUnion(sa, ub)

	case *ParenSelect:
		pb, ok := b.(*ParenSelect)
		if !ok {
			return false
		}
		return compareParenSelect(sa, pb)

	default:
		// Fallback to string comparison for unknown types
		return String(a) == String(b)
	}
}

func compareSelect(a, b *Select) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	// Compare all fields
	if a.Cache != b.Cache {
		return false
	}
	if a.Distinct != b.Distinct {
		return false
	}
	if !compareSelectExprs(a.SelectExprs, b.SelectExprs) {
		return false
	}
	if !compareTableExprs(a.From, b.From) {
		return false
	}
	if !compareWhere(a.Where, b.Where) {
		return false
	}
	if !compareGroupBy(a.GroupBy, b.GroupBy) {
		return false
	}
	if !compareWhere(a.Having, b.Having) {
		return false
	}
	if !compareOrderBy(a.OrderBy, b.OrderBy) {
		return false
	}
	// Note: Limit and Lock comparison could be added if needed
	if a.Lock != b.Lock {
		return false
	}

	return true
}

func compareUnion(a, b *Union) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	if a.Type != b.Type {
		return false
	}
	if !compareSelectStatement(a.Left, b.Left) {
		return false
	}
	if !compareSelectStatement(a.Right, b.Right) {
		return false
	}
	if !compareOrderBy(a.OrderBy, b.OrderBy) {
		return false
	}
	if a.Lock != b.Lock {
		return false
	}

	return true
}

func compareParenSelect(a, b *ParenSelect) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	return compareSelectStatement(a.Select, b.Select)
}

func compareSelectExprs(a, b SelectExprs) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if !compareSelectExpr(a[i], b[i]) {
			return false
		}
	}

	return true
}

func compareSelectExpr(a, b SelectExpr) bool {
	switch ae := a.(type) {
	case *StarExpr:
		be, ok := b.(*StarExpr)
		if !ok {
			return false
		}
		return compareStarExpr(ae, be)

	case *AliasedExpr:
		be, ok := b.(*AliasedExpr)
		if !ok {
			return false
		}
		return compareAliasedExpr(ae, be)

	default:
		// Fallback to string comparison
		return String(a) == String(b)
	}
}

func compareStarExpr(a, b *StarExpr) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	return String(a.TableName) == String(b.TableName)
}

func compareAliasedExpr(a, b *AliasedExpr) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	if !compareExpr(a.Expr, b.Expr) {
		return false
	}

	// Compare aliases - convert to string for comparison
	return String(a.As) == String(b.As)
}

func compareTableExprs(a, b TableExprs) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if !compareTableExpr(a[i], b[i]) {
			return false
		}
	}

	return true
}

func compareTableExpr(a, b TableExpr) bool {
	// For simplicity, use string comparison for table expressions
	// This could be enhanced with structural comparison if needed
	return String(a) == String(b)
}

func compareWhere(a, b *Where) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	if a.Type != b.Type {
		return false
	}

	return compareExpr(a.Expr, b.Expr)
}

func compareGroupBy(a, b GroupBy) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if !compareExpr(a[i], b[i]) {
			return false
		}
	}

	return true
}

func compareOrderBy(a, b OrderBy) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if !compareOrder(a[i], b[i]) {
			return false
		}
	}

	return true
}

func compareOrder(a, b *Order) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	if a.Direction != b.Direction {
		return false
	}

	return compareExpr(a.Expr, b.Expr)
}

// CompareExpr compares two expressions after normalizing them.
func CompareExpr(a, b Expr) bool {
	normA := NormalizeExpr(a)
	normB := NormalizeExpr(b)
	return compareExpr(normA, normB)
}

func compareExpr(a, b Expr) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	// Check if both expressions have the same type
	if reflect.TypeOf(a) != reflect.TypeOf(b) {
		return false
	}

	// Compare based on type
	switch ae := a.(type) {
	case *AndExpr:
		be := b.(*AndExpr)
		return compareExpr(ae.Left, be.Left) && compareExpr(ae.Right, be.Right)

	case *OrExpr:
		be := b.(*OrExpr)
		return compareExpr(ae.Left, be.Left) && compareExpr(ae.Right, be.Right)

	case *NotExpr:
		be := b.(*NotExpr)
		return compareExpr(ae.Expr, be.Expr)

	case *ParenExpr:
		be := b.(*ParenExpr)
		return compareExpr(ae.Expr, be.Expr)

	case *ComparisonExpr:
		be := b.(*ComparisonExpr)
		return ae.Operator == be.Operator &&
			compareExpr(ae.Left, be.Left) &&
			compareExpr(ae.Right, be.Right)

	case *IsExpr:
		be := b.(*IsExpr)
		return ae.Operator == be.Operator && compareExpr(ae.Expr, be.Expr)

	case *BinaryExpr:
		be := b.(*BinaryExpr)
		return ae.Operator == be.Operator &&
			compareExpr(ae.Left, be.Left) &&
			compareExpr(ae.Right, be.Right)

	case *UnaryExpr:
		be := b.(*UnaryExpr)
		return ae.Operator == be.Operator && compareExpr(ae.Expr, be.Expr)

	case *CaseExpr:
		be := b.(*CaseExpr)
		return compareCaseExpr(ae, be)

	case *CastExpr:
		be := b.(*CastExpr)
		return compareCastExpr(ae, be)

	case *FuncExpr:
		be := b.(*FuncExpr)
		return compareFuncExpr(ae, be)

	case *FuncCallExpr:
		be := b.(*FuncCallExpr)
		return compareFuncCallExpr(ae, be)

	case *ColName:
		be := b.(*ColName)
		return compareColName(ae, be)

	case *SQLVal:
		be := b.(*SQLVal)
		return compareSQLVal(ae, be)

	case *NullVal:
		// NullVal has no fields, they're always equal
		return true

	case BoolVal:
		be := b.(BoolVal)
		return ae == be

	case *Subquery:
		be := b.(*Subquery)
		return compareSelectStatement(ae.Select, be.Select)

	case ValTuple:
		be := b.(ValTuple)
		return compareValTuple(ae, be)

	case ListArg:
		be := b.(ListArg)
		return bytes.Equal(ae, be)

	default:
		// Fallback to string comparison for unknown types
		return String(a) == String(b)
	}
}

func compareCaseExpr(a, b *CaseExpr) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	if !compareExpr(a.Expr, b.Expr) {
		return false
	}

	if len(a.Whens) != len(b.Whens) {
		return false
	}

	for i := range a.Whens {
		if !compareExpr(a.Whens[i].Cond, b.Whens[i].Cond) {
			return false
		}
		if !compareExpr(a.Whens[i].Val, b.Whens[i].Val) {
			return false
		}
	}

	return compareExpr(a.Else, b.Else)
}

func compareCastExpr(a, b *CastExpr) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	if !compareExpr(a.Expr, b.Expr) {
		return false
	}

	// Compare types - for now use string comparison
	return String(a.Type) == String(b.Type)
}

func compareFuncExpr(a, b *FuncExpr) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	if String(a.Name) != String(b.Name) {
		return false
	}

	if a.Distinct != b.Distinct {
		return false
	}

	return compareSelectExprs(a.Exprs, b.Exprs)
}

func compareFuncCallExpr(a, b *FuncCallExpr) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	if String(a.Name) != String(b.Name) {
		return false
	}

	if len(a.Exprs) != len(b.Exprs) {
		return false
	}

	for i := range a.Exprs {
		if !compareExpr(a.Exprs[i], b.Exprs[i]) {
			return false
		}
	}

	return true
}

func compareColName(a, b *ColName) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	// For column names, use string comparison since we normalize table qualifiers
	return String(a) == String(b)
}

func compareSQLVal(a, b *SQLVal) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	if a.Type != b.Type {
		return false
	}

	return bytes.Equal(a.Val, b.Val)
}

func compareValTuple(a, b ValTuple) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if !compareExpr(a[i], b[i]) {
			return false
		}
	}

	return true
}

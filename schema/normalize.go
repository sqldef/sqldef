package schema

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/sqldef/sqldef/v3/parser"
	"github.com/sqldef/sqldef/v3/util"
)

var (
	dataTypeAliases = map[string]string{
		"bool":    "boolean",
		"int":     "integer",
		"char":    "character",
		"numeric": "decimal",
		"varchar": "character varying",
	}
	postgresDataTypeAliases = map[string]string{
		"int2":   "smallint",
		"int4":   "integer",
		"int8":   "bigint",
		"float4": "real",
		"float8": "double precision",
		"float":  "double precision",
		"bpchar": "character",

		// Timezone type aliases (the timezone flag is stored separately in Column.timezone)
		"timestamptz":                 "timestamp",
		"timestamp with time zone":    "timestamp",
		"timestamp without time zone": "timestamp",
		"timetz":                      "time",
		"time with time zone":         "time",
		"time without time zone":      "time",
	}
	mssqlDataTypeAliases = map[string]string{}
	mysqlDataTypeAliases = map[string]string{
		"boolean": "tinyint",
	}
)

// normalizeTypeName normalizes a type name using dataTypeAliases and mode-specific aliases.
// This is the central function for all type name normalization in the generator.
func normalizeTypeName(typeName string, mode GeneratorMode) string {
	// Normalize to lowercase for case-insensitive comparison
	normalized := strings.ToLower(typeName)

	// Apply common aliases
	if alias, ok := dataTypeAliases[normalized]; ok {
		normalized = alias
	}

	// Apply database-specific aliases
	switch mode {
	case GeneratorModePostgres:
		if alias, ok := postgresDataTypeAliases[normalized]; ok {
			normalized = alias
		}
	case GeneratorModeMysql:
		if alias, ok := mysqlDataTypeAliases[normalized]; ok {
			normalized = alias
		}
	case GeneratorModeMssql:
		if alias, ok := mssqlDataTypeAliases[normalized]; ok {
			normalized = alias
		}
	}

	return normalized
}

// normalizeConvertType normalizes a ConvertType's type name.
// This handles type aliases like int -> integer and properly handles array types like int[] -> integer[]
func normalizeConvertType(convertType *parser.ConvertType, mode GeneratorMode) *parser.ConvertType {
	if convertType == nil {
		return nil
	}

	// Check if the type is an array type (ends with [])
	typeStr := convertType.Type
	isArray := strings.HasSuffix(typeStr, "[]")

	// For array types, normalize the base type and then re-append the []
	if isArray {
		baseType := strings.TrimSuffix(typeStr, "[]")
		normalizedBase := normalizeTypeName(baseType, mode)
		typeStr = normalizedBase + "[]"
	} else {
		typeStr = normalizeTypeName(typeStr, mode)
	}

	return &parser.ConvertType{
		Type:     typeStr,
		Length:   convertType.Length,
		Scale:    convertType.Scale,
		Operator: convertType.Operator,
		Charset:  convertType.Charset,
	}
}

// BuildPostgresConstraintName generates a constraint name following PostgreSQL's naming convention.
// It automatically truncates names to 63 characters (NAMEDATALEN - 1) using PostgreSQL's algorithm:
// - If column > 28 chars: reduce column to 28 first, then apply remaining overflow to table
// - If column == 28 chars and table <= 29 chars: truncate table
// - If column == 28 chars and table > 29 chars: truncate table
// - If column < 28 chars: truncate table
// In summary: when column <= 28, always truncate the table first
func buildPostgresConstraintName(tableName, columnName, suffix string) string {
	fullName := fmt.Sprintf("%s_%s_%s", tableName, columnName, suffix)
	if len(fullName) <= 63 {
		return fullName
	}

	overflow := len(fullName) - 63
	tableLen := len(tableName)
	columnLen := len(columnName)

	tableRemove := 0
	columnRemove := 0

	if columnLen > 28 {
		// Column exceeds 28: reduce to 28 first, then put remaining overflow on table
		columnRemove = overflow
		if columnRemove > columnLen-28 {
			// Column can only be reduced to 28, put the rest on table
			tableRemove = columnRemove - (columnLen - 28)
			columnRemove = columnLen - 28
		}
	} else {
		// Column <= 28: always truncate table
		tableRemove = overflow
	}

	truncatedTable := tableName[:tableLen-tableRemove]
	truncatedColumn := columnName[:columnLen-columnRemove]

	return fmt.Sprintf("%s_%s_%s", truncatedTable, truncatedColumn, suffix)
}

// normalizeCheckExpr normalizes a CHECK constraint expression AST for comparison
// mode parameter controls PostgreSQL-specific normalization (IN to ANY conversion)
func normalizeCheckExpr(expr parser.Expr, mode GeneratorMode) parser.Expr {
	if expr == nil {
		return nil
	}

	switch e := expr.(type) {
	case *parser.CastExpr:
		// Remove certain casts that PostgreSQL simplifies
		// - text, character varying: Always removed
		// - date, timestamp without time zone: Removed when cast from string literals
		// - time, time without time zone: Kept but normalized (PostgreSQL preserves these for precision)
		if e.Type != nil {
			typeStr := strings.ToLower(e.Type.Type)

			// Always remove text casts
			if typeStr == "text" || typeStr == "character varying" {
				return normalizeCheckExpr(e.Expr, mode)
			}

			normalizedTypeStr := normalizeTypeName(typeStr, mode)

			// Remove date/timestamp casts from string literals
			// PostgreSQL simplifies '2020-01-01'::date to '2020-01-01' in CHECK constraints
			// But keeps time casts: time '09:00:00' becomes '09:00:00'::time
			if normalizedTypeStr == "date" || normalizedTypeStr == "timestamp" {
				return normalizeCheckExpr(e.Expr, mode)
			}

			// For time types, keep the cast but use normalized type name
			return &parser.CastExpr{
				Expr: normalizeCheckExpr(e.Expr, mode),
				Type: &parser.ConvertType{Type: normalizedTypeStr},
			}
		}
		return &parser.CastExpr{
			Expr: normalizeCheckExpr(e.Expr, mode),
			Type: e.Type,
		}
	case *parser.ParenExpr:
		normalized := normalizeCheckExpr(e.Expr, mode)
		if paren, ok := normalized.(*parser.ParenExpr); ok {
			return paren
		}
		// Unwrap parentheses around simple expressions (literals, column names, etc.)
		// MSSQL/PostgreSQL may add unnecessary parens like (1) instead of 1 or (name) instead of name
		switch normalized.(type) {
		case *parser.SQLVal, *parser.ColName:
			return normalized
		}
		return &parser.ParenExpr{Expr: normalized}
	case *parser.AndExpr:
		// Normalize operands and unwrap unnecessary parentheses around them
		left := normalizeCheckExpr(e.Left, mode)
		right := normalizeCheckExpr(e.Right, mode)
		// MySQL adds parentheses around each operand in AND chains, so unwrap them
		left = unwrapOutermostParenExpr(left)
		right = unwrapOutermostParenExpr(right)
		return &parser.AndExpr{
			Left:  left,
			Right: right,
		}
	case *parser.OrExpr:
		// Normalize operands and unwrap unnecessary parentheses around them
		left := normalizeCheckExpr(e.Left, mode)
		right := normalizeCheckExpr(e.Right, mode)
		// MySQL adds parentheses around each operand in OR chains, so unwrap them
		// Always safe to unwrap in OR chains since OR has the lowest precedence
		left = unwrapOutermostParenExpr(left)
		right = unwrapOutermostParenExpr(right)

		// Try to convert OR chain of equality comparisons to IN expression
		// MSSQL transforms IN (a, b, c) to col=a OR col=b OR col=c
		// We normalize back to IN for comparison
		if inExpr := tryConvertOrChainToIn(&parser.OrExpr{Left: left, Right: right}); inExpr != nil {
			return inExpr
		}

		return &parser.OrExpr{
			Left:  left,
			Right: right,
		}
	case *parser.NotExpr:
		return &parser.NotExpr{Expr: normalizeCheckExpr(e.Expr, mode)}
	case *parser.ComparisonExpr:
		left := normalizeCheckExpr(e.Left, mode)
		right := normalizeCheckExpr(e.Right, mode)
		op := normalizeOperator(e.Operator, mode)
		anyFlag := e.Any
		allFlag := e.All

		// The generic parser may parse "= ANY(ARRAY[...])" as a FuncExpr on the right side
		// We need to normalize this to set the Any/All flags properly
		if funcExpr, ok := right.(*parser.FuncExpr); ok {
			funcName := strings.ToLower(funcExpr.Name.String())
			switch funcName {
			case "any", "some":
				// Convert "column = ANY(array)" to ComparisonExpr with Any=true
				if len(funcExpr.Exprs) == 1 {
					if aliased, ok := funcExpr.Exprs[0].(*parser.AliasedExpr); ok {
						right = normalizeCheckExpr(aliased.Expr, mode)
						anyFlag = true
					}
				}
			case "all":
				// Convert "column = ALL(array)" to ComparisonExpr with All=true
				if len(funcExpr.Exprs) == 1 {
					if aliased, ok := funcExpr.Exprs[0].(*parser.AliasedExpr); ok {
						right = normalizeCheckExpr(aliased.Expr, mode)
						allFlag = true
					}
				}
			}
		}

		// Unwrap ParenExpr from right side for ANY/ALL to ensure consistent formatting
		// The parser may create ParenExpr(ArrayConstructor) which formats as ANY(ARRAY
		// We want to normalize to ArrayConstructor directly which formats as ANY (ARRAY
		if anyFlag || allFlag {
			if parenExpr, ok := right.(*parser.ParenExpr); ok {
				right = parenExpr.Expr
			}
		}

		// Handle IN clauses based on mode
		if op == "in" || op == "not in" {
			if tuple, ok := right.(parser.ValTuple); ok {
				if mode == GeneratorModePostgres {
					// PostgreSQL normalizes IN (values) to = ANY (ARRAY[values])

					elements := sortAndDeduplicateValues(tuple)
					normalizedElements := util.TransformSlice(elements, func(elem parser.Expr) parser.Expr {
						return normalizeCheckExpr(elem, mode)
					})
					right = &parser.ArrayConstructor{Elements: normalizedElements}

					// Change operator and set ANY flag
					if op == "in" {
						op = "="
						anyFlag = true
					} else { // "not in"
						op = "!="
						anyFlag = true
					}
				} else {
					// For other databases, keep IN but sort the tuple for consistent comparison
					sortedElements := sortAndDeduplicateValues(tuple)
					normalizedElements := util.TransformSlice(sortedElements, func(elem parser.Expr) parser.Expr {
						return normalizeCheckExpr(elem, mode)
					})
					right = parser.ValTuple(normalizedElements)
				}
			}
		}

		// For ANY/ALL expressions with ValTuple, sort and deduplicate
		if (anyFlag || allFlag) && !e.Any && !e.All {
			// This means we just set the flag above from IN conversion
			// Already handled
		} else if anyFlag || allFlag {
			// Normalize existing ANY/ALL expressions (strip casts, preserve order)
			if arrayConst, ok := right.(*parser.ArrayConstructor); ok {
				normalizedElements := util.TransformSlice(arrayConst.Elements, func(elem parser.Expr) parser.Expr {
					return normalizeCheckExpr(elem, mode)
				})
				right = &parser.ArrayConstructor{Elements: normalizedElements}
			}
		}

		return &parser.ComparisonExpr{
			Operator: op,
			Left:     left,
			Right:    right,
			Escape:   normalizeCheckExpr(e.Escape, mode),
			All:      allFlag,
			Any:      anyFlag,
		}
	case *parser.BinaryExpr:
		return &parser.BinaryExpr{
			Operator: e.Operator,
			Left:     normalizeCheckExpr(e.Left, mode),
			Right:    normalizeCheckExpr(e.Right, mode),
		}
	case *parser.UnaryExpr:
		return &parser.UnaryExpr{
			Operator: e.Operator,
			Expr:     normalizeCheckExpr(e.Expr, mode),
		}
	case *parser.FuncExpr:
		normalizedExprs := util.TransformSlice(e.Exprs, func(arg parser.SelectExpr) parser.SelectExpr {
			if aliased, ok := arg.(*parser.AliasedExpr); ok {
				return &parser.AliasedExpr{
					Expr: normalizeCheckExpr(aliased.Expr, mode),
					As:   aliased.As,
				}
			}
			return arg
		})
		// Normalize function name to lowercase (PostgreSQL convention)
		funcName := parser.NewColIdent(strings.ToLower(e.Name.String()))
		return &parser.FuncExpr{
			Qualifier: e.Qualifier,
			Name:      funcName,
			Distinct:  e.Distinct,
			Exprs:     normalizedExprs,
			Over:      e.Over,
		}
	case *parser.ArrayConstructor:
		normalizedElements := util.TransformSlice(e.Elements, func(elem parser.Expr) parser.Expr {
			return normalizeCheckExpr(elem, mode)
		})
		return &parser.ArrayConstructor{Elements: normalizedElements}
	case *parser.IsExpr:
		return &parser.IsExpr{
			Operator: e.Operator,
			Expr:     normalizeCheckExpr(e.Expr, mode),
		}
	case *parser.RangeCond:
		return &parser.RangeCond{
			Operator: e.Operator,
			Left:     normalizeCheckExpr(e.Left, mode),
			From:     normalizeCheckExpr(e.From, mode),
			To:       normalizeCheckExpr(e.To, mode),
		}
	case parser.ValTuple:
		normalizedTuple := util.TransformSlice(e, func(elem parser.Expr) parser.Expr {
			return normalizeCheckExpr(elem, mode)
		})
		return parser.ValTuple(normalizedTuple)
	case *parser.ColName:
		qualifierStr := ""
		if e.Qualifier.Name.String() != "" {
			qualifierStr = normalizeName(e.Qualifier.Name.String())
		}
		nameStr := normalizeName(e.Name.String())

		return &parser.ColName{
			Name: parser.NewColIdent(nameStr),
			Qualifier: parser.TableName{
				Name: parser.NewTableIdent(qualifierStr),
			},
		}
	case *parser.TypedLiteral:
		// PostgreSQL normalizes typed literals differently based on type:
		// - DATE 'value' -> 'value' (removes type prefix)
		// - TIMESTAMP 'value' -> 'value' (removes type prefix)
		// - TIME 'value' -> 'value'::time (converts to cast expression)
		// - TIMETZ 'value' -> 'value'::timetz (converts to cast expression)
		typeStr := strings.ToLower(e.Type)

		// For time types, convert to cast expression
		if strings.HasPrefix(typeStr, "time") {
			return &parser.CastExpr{
				Expr: normalizeCheckExpr(e.Value, mode),
				Type: &parser.ConvertType{Type: typeStr},
			}
		}

		// For date/timestamp, remove the type prefix
		return normalizeCheckExpr(e.Value, mode)
	default:
		// For all other expression types (literals, etc.), return as-is
		return expr
	}
}

// normalizeExpr normalizes an expression.
// This is similar to normalizeCheckExpr but tailored for other contexts.
func normalizeExpr(expr parser.Expr, mode GeneratorMode) parser.Expr {
	if expr == nil {
		return nil
	}

	switch e := expr.(type) {
	case *parser.ColName:
		// Normalize column name and qualifier
		// 1. Remove database-specific quotes/brackets from identifiers
		// 2. For Postgres, remove table qualifiers from column references
		qualifierStr := ""
		if e.Qualifier.Name.String() != "" {
			qualifierStr = normalizeName(e.Qualifier.Name.String())
		}
		nameStr := normalizeName(e.Name.String())

		// For Postgres, remove table qualifiers (e.g., "users.name" -> "name")
		if mode == GeneratorModePostgres {
			qualifierStr = ""
		}

		return &parser.ColName{
			Name: parser.NewColIdent(nameStr),
			Qualifier: parser.TableName{
				Name: parser.NewTableIdent(qualifierStr),
			},
		}
	case *parser.ArrayConstructor:
		elements := util.TransformSlice(e.Elements, func(elem parser.Expr) parser.Expr {
			return normalizeExpr(elem, mode)
		})
		return &parser.ArrayConstructor{Elements: elements}
	case *parser.FuncExpr:
		// For PostgreSQL, normalize date/time function calls to keywords
		// The generic parser parses CURRENT_DATE in parentheses as a function call,
		// but without parentheses as a keyword (SQLVal with ValArg type)
		// e.g., (CURRENT_DATE) -> current_date(), but CURRENT_DATE -> current_date
		if mode == GeneratorModePostgres && len(e.Exprs) == 0 {
			funcName := strings.ToLower(e.Name.String())
			switch funcName {
			case "current_date", "current_time", "current_timestamp":
				return parser.NewValArg([]byte(funcName))
			}
		}

		normalizedExprs := parser.SelectExprs{}
		for _, arg := range e.Exprs {
			// For Postgres, check for ARRAY constructors BEFORE normalizing
			// PostgreSQL standardizes function arguments to use ARRAY['a', 'b'] notation
			// but users may write them expanded as 'a', 'b', so we expand for comparison
			// e.g., jsonb_extract_path_text(payload, ARRAY['amount']) -> jsonb_extract_path_text(payload, 'amount')
			// e.g., jsonb_extract_path_text(payload, ARRAY['a', 'b']) -> jsonb_extract_path_text(payload, 'a', 'b')
			if mode == GeneratorModePostgres {
				if aliased, ok := arg.(*parser.AliasedExpr); ok {
					if arrayConstr, ok := aliased.Expr.(*parser.ArrayConstructor); ok && len(arrayConstr.Elements) > 0 {
						// Expand ARRAY elements into separate normalized arguments
						for _, elem := range arrayConstr.Elements {
							normalizedExprs = append(normalizedExprs, &parser.AliasedExpr{
								Expr: normalizeExpr(elem, mode),
							})
						}
						continue
					}
				}
			}

			// Not an ARRAY, normalize normally
			normalized := normalizeSelectExpr(arg, mode)
			normalizedExprs = append(normalizedExprs, normalized)
		}
		return &parser.FuncExpr{
			Qualifier: e.Qualifier,
			Name:      e.Name,
			Distinct:  e.Distinct,
			Exprs:     normalizedExprs,
			Over:      e.Over,
		}
	case *parser.CastExpr:
		normalizedExpr := normalizeExpr(e.Expr, mode)
		// For PostgreSQL, unwrap unnecessary parentheses around simple expressions in casts
		// PostgreSQL adds parentheses like (amount)::numeric, but we want to normalize to amount::numeric
		if mode == GeneratorModePostgres {
			if parenExpr, ok := normalizedExpr.(*parser.ParenExpr); ok {
				if _, isColName := parenExpr.Expr.(*parser.ColName); isColName {
					normalizedExpr = parenExpr.Expr
				}
			}

			// Remove redundant casts that PostgreSQL adds for typed literals
			// PostgreSQL adds ::type casts when storing typed literals like DATE '2024-01-01'
			// We strip these redundant casts to generate cleaner DDL
			// However, we preserve necessary casts like ::interval, ::bpchar, ::json, ::jsonb, and numeric casts
			if e.Type != nil {
				typeStr := strings.ToLower(e.Type.Type)
				// Only strip casts on simple string literals (not in expressions)
				if sqlVal, ok := normalizedExpr.(*parser.SQLVal); ok && sqlVal.Type == parser.StrVal {
					// Check if the string looks like a number (PostgreSQL stores negative numbers as string literals with casts)
					// Only treat it as numeric if it's purely digits/decimal, not a date/time string
					if util.IsNumericString(string(sqlVal.Val)) {
						// PostgreSQL stores negative numbers as string literals with casts like '-20'::integer
						// We need to convert these back to plain numeric literals
						switch typeStr {
						case "integer", "bigint", "smallint":
							// Convert numeric string to actual numeric literal
							// This unwraps '-20'::integer -> -20
							return parser.NewIntVal(sqlVal.Val)
						case "numeric", "decimal", "real", "double precision":
							return parser.NewFloatVal(sqlVal.Val)
						}
					} else {
						// Strip redundant type casts that PostgreSQL adds on non-numeric strings
						switch typeStr {
						case "text", "character varying":
							// Always strip text casts on non-numeric strings
							return normalizedExpr
						case "date", "time", "timestamp", "timestamp without time zone":
							// Strip date/time casts on literals (PostgreSQL adds these for typed literals)
							return normalizedExpr
						}
					}
				}
				// Strip redundant casts on NULL values and normalize to lowercase
				// PostgreSQL stores DEFAULT NULL as NULL::type, but we normalize to just null
				// (matching the lexer's keyword normalization to lowercase)
				if sqlVal, ok := normalizedExpr.(*parser.SQLVal); ok && sqlVal.Type == parser.ValArg {
					if strings.EqualFold(string(sqlVal.Val), "null") {
						// Strip all type casts on NULL and return lowercase null (matching lexer)
						return parser.NewValArg([]byte("null"))
					}
				}
				// Preserve all other casts (interval, bpchar, json, jsonb, etc.)

				// Remove redundant implicit casts that PostgreSQL adds for function arguments
				// e.g., expr::bigint::double precision -> expr::bigint
				// PostgreSQL adds these when a function expects double precision but gets bigint
				if typeStr == "double precision" || typeStr == "real" {
					if innerCast, ok := normalizedExpr.(*parser.CastExpr); ok {
						innerTypeStr := strings.ToLower(innerCast.Type.Type)
						// If the inner cast is to an integer type, remove the outer double precision cast
						if innerTypeStr == "bigint" || innerTypeStr == "integer" || innerTypeStr == "smallint" {
							return innerCast
						}
					}
				}

				// Remove redundant array typecasts on ARRAY constructors
				// PostgreSQL normalizes ARRAY[expr::type]::type[] to ARRAY[(expr)::type]
				// The array typecast is redundant since the ARRAY constructor already produces the right type
				// e.g., ARRAY[current_date::text]::text[] -> ARRAY[(CURRENT_DATE)::text]
				if _, isArrayConstructor := normalizedExpr.(*parser.ArrayConstructor); isArrayConstructor {
					// Check if this is an array type cast (type string ends with [])
					if strings.HasSuffix(typeStr, "[]") {
						// This is an array type (e.g., text[], int[])
						// Strip the redundant array typecast and return just the ARRAY constructor
						return normalizedExpr
					}
				}
			}
		}

		// Normalize the type name in the cast expression to handle type aliases
		// e.g., int[]::int[] should become int[]::integer[]
		normalizedType := normalizeConvertType(e.Type, mode)

		return &parser.CastExpr{
			Expr: normalizedExpr,
			Type: normalizedType,
		}
	case *parser.ParenExpr:
		normalizedInner := normalizeExpr(e.Expr, mode)
		// For PostgreSQL, unwrap parentheses around most expressions to normalize
		// PostgreSQL adds parentheses around many expressions, but we want a canonical form
		// We always unwrap ParenExpr during normalization to get a canonical form
		// The only exception is when parentheses are around complex nested expressions
		// where they're needed for precedence (like CASE inside a larger expression)
		if mode == GeneratorModePostgres {
			// Preserve parentheses around COLLATE expressions, as they're semantically significant
			if _, isCollate := normalizedInner.(*parser.CollateExpr); isCollate {
				return &parser.ParenExpr{
					Expr: normalizedInner,
				}
			}
			// Always unwrap single-layer parentheses for normalization
			// This handles cases like (NOT deleted), (a = 1), ((col)::type), etc.
			return normalizedInner
		}
		return &parser.ParenExpr{
			Expr: normalizedInner,
		}
	case *parser.ComparisonExpr:
		return &parser.ComparisonExpr{
			Operator: e.Operator,
			Left:     normalizeExpr(e.Left, mode),
			Right:    normalizeExpr(e.Right, mode),
			Escape:   normalizeExpr(e.Escape, mode),
			All:      e.All,
			Any:      e.Any,
		}
	case *parser.AndExpr:
		return &parser.AndExpr{
			Left:  normalizeExpr(e.Left, mode),
			Right: normalizeExpr(e.Right, mode),
		}
	case *parser.OrExpr:
		return &parser.OrExpr{
			Left:  normalizeExpr(e.Left, mode),
			Right: normalizeExpr(e.Right, mode),
		}
	case *parser.BinaryExpr:
		return &parser.BinaryExpr{
			Operator: e.Operator,
			Left:     normalizeExpr(e.Left, mode),
			Right:    normalizeExpr(e.Right, mode),
		}
	case *parser.UnaryExpr:
		normalized := normalizeExpr(e.Expr, mode)

		// Collapse UnaryExpr with minus/plus on numeric literals to SQLVal
		// This ensures "-20" and "- 20" (unary minus on 20) are treated the same
		if sqlVal, ok := normalized.(*parser.SQLVal); ok {
			switch e.Operator {
			case parser.UMinusStr:
				switch sqlVal.Type {
				case parser.IntVal:
					// Create negative integer: -N
					if sqlVal.Val[0] == '-' {
						// Double negative: --N → N
						return parser.NewIntVal(sqlVal.Val[1:])
					} else {
						return parser.NewIntVal(append([]byte("-"), sqlVal.Val...))
					}
				case parser.FloatVal:
					// Create negative float: -N.M
					if sqlVal.Val[0] == '-' {
						// Double negative: --N.M → N.M
						return parser.NewFloatVal(sqlVal.Val[1:])
					} else {
						return parser.NewFloatVal(append([]byte("-"), sqlVal.Val...))
					}
				}
			case parser.UPlusStr:
				// Unary plus has no effect on numeric values
				switch sqlVal.Type {
				case parser.IntVal, parser.FloatVal:
					return sqlVal
				}
			}
		}

		return &parser.UnaryExpr{
			Operator: e.Operator,
			Expr:     normalized,
		}
	case *parser.IsExpr:
		return &parser.IsExpr{
			Operator: e.Operator,
			Expr:     normalizeExpr(e.Expr, mode),
		}
	case *parser.ConvertExpr:
		// Normalize CAST(expr AS type) to expr::type (CastExpr) for consistency
		// PostgreSQL represents both forms identically in its internal representation
		if mode == GeneratorModePostgres && e.Action == parser.CastStr {
			return normalizeExpr(&parser.CastExpr{
				Expr: e.Expr,
				Type: e.Type,
			}, mode)
		}
		return &parser.ConvertExpr{
			Action: e.Action,
			Expr:   normalizeExpr(e.Expr, mode),
			Type:   e.Type,
			Style:  e.Style,
		}
	case *parser.CollateExpr:
		return &parser.CollateExpr{
			Expr:    normalizeExpr(e.Expr, mode),
			Charset: strings.ToLower(e.Charset),
		}
	case *parser.CaseExpr:
		normalizedWhens := make([]*parser.When, len(e.Whens))
		for i, when := range e.Whens {
			normalizedWhens[i] = &parser.When{
				Cond: normalizeExpr(when.Cond, mode),
				Val:  normalizeExpr(when.Val, mode),
			}
		}
		normalizedElse := normalizeExpr(e.Else, mode)
		// PostgreSQL adds ELSE NULL to CASE expressions, normalize it away
		if _, ok := normalizedElse.(*parser.NullVal); ok {
			normalizedElse = nil
		}
		// Also handle ELSE NULL::type (cast to null)
		if castExpr, ok := normalizedElse.(*parser.CastExpr); ok {
			if _, isNull := castExpr.Expr.(*parser.NullVal); isNull {
				normalizedElse = nil
			}
		}
		return &parser.CaseExpr{
			Expr:  normalizeExpr(e.Expr, mode),
			Whens: normalizedWhens,
			Else:  normalizedElse,
		}
	case *parser.TypedLiteral:
		// PostgreSQL normalizes typed literals by removing the type prefix
		// e.g., DATE '2024-01-01' -> '2024-01-01'
		// e.g., TIME '12:00:00' -> '12:00:00'
		// e.g., TIMESTAMP '2024-01-01 12:00:00' -> '2024-01-01 12:00:00'
		// Only normalize for PostgreSQL mode
		if mode == GeneratorModePostgres {
			return normalizeExpr(e.Value, mode)
		}
		return expr
	default:
		// For literals and other types, return as-is
		return expr
	}
}

// normalizeSelectExprs normalizes SELECT expressions for comparison
func normalizeSelectExprs(exprs parser.SelectExprs, mode GeneratorMode) parser.SelectExprs {
	normalized := make(parser.SelectExprs, len(exprs))
	for i, expr := range exprs {
		normalized[i] = normalizeSelectExpr(expr, mode)
	}
	return normalized
}

// normalizeSelectExpr normalizes a single SELECT expression for views
func normalizeSelectExpr(expr parser.SelectExpr, mode GeneratorMode) parser.SelectExpr {
	switch e := expr.(type) {
	case *parser.AliasedExpr:
		return &parser.AliasedExpr{
			Expr: normalizeExpr(e.Expr, mode),
			As:   e.As,
		}
	case *parser.StarExpr:
		return e
	default:
		return expr
	}
}

// normalizeTableExprs normalizes FROM clause table expressions
func normalizeTableExprs(exprs parser.TableExprs, mode GeneratorMode) parser.TableExprs {
	// For now, return as-is since table name normalization is less critical
	// We mainly care about column references in the SELECT and WHERE clauses
	return exprs
}

// normalizeWhere normalizes WHERE clause
func normalizeWhere(where *parser.Where, mode GeneratorMode) *parser.Where {
	if where == nil {
		return nil
	}
	return &parser.Where{
		Type: where.Type,
		Expr: normalizeExpr(where.Expr, mode),
	}
}

// normalizeGroupBy normalizes GROUP BY clause
func normalizeGroupBy(groupBy parser.GroupBy, mode GeneratorMode) parser.GroupBy {
	normalized := make(parser.GroupBy, len(groupBy))
	for i, expr := range groupBy {
		normalized[i] = normalizeExpr(expr, mode)
	}
	return normalized
}

// normalizeOrderBy normalizes ORDER BY clause
func normalizeOrderBy(orderBy parser.OrderBy, mode GeneratorMode) parser.OrderBy {
	normalized := make(parser.OrderBy, len(orderBy))
	for i, order := range orderBy {
		normalized[i] = &parser.Order{
			Expr:      normalizeExpr(order.Expr, mode),
			Direction: order.Direction,
		}
	}
	return normalized
}

// normalizeWith normalizes a WITH clause (Common Table Expressions) for comparison.
func normalizeWith(with *parser.With, mode GeneratorMode) *parser.With {
	if with == nil {
		return nil
	}

	normalizedCTEs := make([]*parser.CommonTableExpr, len(with.CTEs))
	for i, cte := range with.CTEs {
		normalizedCTEs[i] = &parser.CommonTableExpr{
			Name:       cte.Name,
			Columns:    cte.Columns,
			Definition: normalizeViewDefinition(cte.Definition, mode),
		}
	}

	return &parser.With{
		CTEs: normalizedCTEs,
	}
}

// normalizeViewDefinition normalizes a view definition AST for comparison.
// This function removes database-specific formatting differences that don't affect the logical meaning.
func normalizeViewDefinition(stmt parser.SelectStatement, mode GeneratorMode) parser.SelectStatement {
	if stmt == nil {
		return nil
	}

	switch s := stmt.(type) {
	case *parser.Select:
		return &parser.Select{
			Cache:       s.Cache,
			Comments:    nil, // Remove comments for view comparison - they don't affect semantic meaning
			Distinct:    s.Distinct,
			Hints:       s.Hints,
			SelectExprs: normalizeSelectExprs(s.SelectExprs, mode),
			From:        normalizeTableExprs(s.From, mode),
			Where:       normalizeWhere(s.Where, mode),
			GroupBy:     normalizeGroupBy(s.GroupBy, mode),
			Having:      normalizeWhere(s.Having, mode),
			OrderBy:     normalizeOrderBy(s.OrderBy, mode),
			Limit:       s.Limit,
			Lock:        s.Lock,
			With:        normalizeWith(s.With, mode),
		}
	case *parser.Union:
		return &parser.Union{
			Type:    s.Type,
			Left:    normalizeViewDefinition(s.Left, mode),
			Right:   normalizeViewDefinition(s.Right, mode),
			OrderBy: normalizeOrderBy(s.OrderBy, mode),
			Limit:   s.Limit,
			Lock:    s.Lock,
			With:    normalizeWith(s.With, mode),
		}
	default:
		return stmt
	}
}

// normalizeViewColumnsFromDefinition extracts and normalizes column names from a view definition.
// This handles differences in how PostgreSQL versions format column names:
// - PostgreSQL 13-15: includes table qualifiers (e.g., "users.id")
// - PostgreSQL 16+: omits unnecessary qualifiers (e.g., "id")
func normalizeViewColumnsFromDefinition(def parser.SelectStatement, mode GeneratorMode) []string {
	if def == nil {
		return nil
	}

	var selectExprs parser.SelectExprs
	switch stmt := def.(type) {
	case *parser.Select:
		selectExprs = stmt.SelectExprs
	default:
		// For other statement types (e.g., UNION), we can't easily extract columns
		return nil
	}

	return util.TransformSlice(selectExprs, func(expr parser.SelectExpr) string {
		normalized := normalizeSelectExpr(expr, mode)
		return strings.ToLower(parser.String(normalized))
	})
}

// normalizeOperator converts operator to lowercase and applies PostgreSQL-specific mappings.
// PostgreSQL stores certain operators in a canonical form:
// - LIKE is stored as ~~
// - NOT LIKE is stored as !~~
// - != is stored as <>
func normalizeOperator(op string, mode GeneratorMode) string {
	op = strings.ToLower(op)

	if mode == GeneratorModePostgres {
		switch op {
		case "like":
			return "~~"
		case "not like":
			return "!~~"
		case "!=":
			return "<>"
		}
	}

	return op
}

// normalizeName lowercases them for consistent comparison.
// TODO: Identifier case-sensitivity varies by RDBMS and settings:
//   - PostgreSQL: case-insensitive by default, case-sensitive when quoted
//   - MySQL: depends on settings, such as lower_case_table_names
//   - MSSQL: depends on collation settings
//     For now, we lowercase everything for normalization.
func normalizeName(name string) string {
	return strings.ToLower(name)
}

// Workaround for: ((current_schema())::uuid = (current_database())::uuid)
// generated by (current_schema()::uuid = current_database()::uuid)
func normalizeUsing(expr string) string {
	expr = strings.ToLower(expr)
	expr = strings.ReplaceAll(expr, "(", "")
	expr = strings.ReplaceAll(expr, ")", "")
	return expr
}

var postgresTablePrivilegeList = []string{
	"DELETE",
	"INSERT",
	"REFERENCES",
	"SELECT",
	"TRIGGER",
	"TRUNCATE",
	"UPDATE",
}

func normalizePrivilegesForComparison(privileges []string) []string {
	if len(privileges) == 1 && (privileges[0] == "ALL" || privileges[0] == "ALL PRIVILEGES") {
		return postgresTablePrivilegeList
	}
	return privileges
}

// Sort privileges in PostgreSQL canonical order
func sortPrivilegesByCanonicalOrder(privileges []string) {
	orderMap := make(map[string]int)
	for i, priv := range postgresTablePrivilegeList {
		orderMap[priv] = i
	}

	slices.SortFunc(privileges, func(a, b string) int {
		orderA, hasA := orderMap[a]
		orderB, hasB := orderMap[b]
		if hasA && hasB {
			return cmp.Compare(orderA, orderB)
		}
		if !hasA && !hasB {
			return cmp.Compare(a, b)
		}
		if hasA {
			return -1
		}
		return 1
	})
}

// sortAndDeduplicateValues sorts and deduplicates a slice of expressions based on their string representation.
// This ensures that semantically equivalent lists are treated as identical regardless of order or duplicates.
// For example: [b, a, b] becomes [a, b]
func sortAndDeduplicateValues[Expr parser.Expr](values []Expr) []Expr {
	if len(values) <= 1 {
		return values
	}

	slices.SortFunc(values, func(a, b Expr) int {
		return cmp.Compare(parser.String(a), parser.String(b))
	})

	uniqueValues := values[:0] // reuse underlying array
	for i, v := range values {
		if i == 0 || parser.String(v) != parser.String(values[i-1]) {
			uniqueValues = append(uniqueValues, v)
		}
	}

	return uniqueValues
}

// tryConvertOrChainToIn attempts to convert an OR chain of equality comparisons
// (e.g., col=a OR col=b OR col=c) into an IN expression (e.g., col IN (a, b, c))
// Returns nil if the conversion is not applicable.
func tryConvertOrChainToIn(orExpr *parser.OrExpr) parser.Expr {
	var column parser.Expr
	var values []parser.Expr

	extractEqComparison := func(expr parser.Expr) (parser.Expr, parser.Expr, bool) {
		cmp, ok := expr.(*parser.ComparisonExpr)
		if !ok || cmp.Operator != "=" {
			return nil, nil, false
		}
		return cmp.Left, cmp.Right, true
	}

	columnsEqual := func(col1, col2 parser.Expr) bool {
		return normalizeName(parser.String(col1)) == normalizeName(parser.String(col2))
	}

	// Walk the OR chain and collect comparisons
	// Also handle already-normalized IN expressions from nested ORs
	var walk func(expr parser.Expr) bool
	walk = func(expr parser.Expr) bool {
		switch e := expr.(type) {
		case *parser.OrExpr:
			return walk(e.Left) && walk(e.Right)
		case *parser.ComparisonExpr:
			// Handle IN expressions that were already normalized
			if strings.EqualFold(e.Operator, "in") {
				if column == nil {
					column = e.Left
				} else if !columnsEqual(column, e.Left) {
					return false
				}
				// Extract values from IN clause
				if tuple, ok := e.Right.(parser.ValTuple); ok {
					for _, v := range tuple {
						values = append(values, v)
					}
					return true
				}
				return false
			}

			col, val, ok := extractEqComparison(e)
			if !ok {
				return false
			}
			if column == nil {
				column = col
				values = append(values, val)
				return true
			}
			if columnsEqual(column, col) {
				values = append(values, val)
				return true
			}
			return false
		default:
			return false
		}
	}

	if !walk(orExpr) || len(values) < 2 {
		return nil
	}

	sortedValues := sortAndDeduplicateValues(values)

	return &parser.ComparisonExpr{
		Operator: "in",
		Left:     column,
		Right:    parser.ValTuple(sortedValues),
	}
}

package schema

import (
	"cmp"
	"fmt"
	"log/slog"
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
	// PostgreSQL serial types to their underlying integer types
	postgresSerialTypes = map[string]string{
		"smallserial": "smallint",
		"serial":      "integer",
		"bigserial":   "bigint",
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

// buildPostgresConstraintNameIdent builds a PostgreSQL auto-generated constraint name
// and returns it as an Ident with quote information inferred from case.
func buildPostgresConstraintNameIdent(tableName, columnName, suffix string) Ident {
	name := buildPostgresConstraintName(tableName, columnName, suffix)
	return NewIdentWithQuoteDetected(name)
}

// buildMysqlForeignKeyName builds a MySQL auto-generated foreign key constraint name
// using the format {table}_{column}_fk (similar to PostgreSQL's {table}_{column}_fkey).
// MySQL's actual auto-generated names are {table}_ibfk_{N} but since we can't predict N,
// we use a column-based deterministic name instead.
func buildMysqlForeignKeyName(tableName, columnName string) string {
	return fmt.Sprintf("%s_%s_fk", tableName, columnName)
}

// buildMysqlForeignKeyNameIdent builds a MySQL auto-generated foreign key constraint name
// and returns it as an Ident.
func buildMysqlForeignKeyNameIdent(tableName, columnName string) Ident {
	name := buildMysqlForeignKeyName(tableName, columnName)
	return NewIdentWithQuoteDetected(name)
}

// buildMssqlForeignKeyName builds a MSSQL auto-generated foreign key constraint name
// using the format FK_{table}_{column}.
// Note: This is NOT the same as MSSQL's native auto-generated names which use
// the format FK__{table}__{column}__{hash} (e.g., "FK__posts__user_id__4CA06362").
// We use a deterministic column-based name for consistency and predictability.
func buildMssqlForeignKeyName(tableName, columnName string) string {
	return fmt.Sprintf("FK_%s_%s", tableName, columnName)
}

// buildMssqlForeignKeyNameIdent builds a MSSQL auto-generated foreign key constraint name
// and returns it as an Ident.
// Note: This is NOT the same as MSSQL's native auto-generated names which include
// a hash suffix. We use a deterministic column-based name for consistency.
func buildMssqlForeignKeyNameIdent(tableName, columnName string) Ident {
	name := buildMssqlForeignKeyName(tableName, columnName)
	return NewIdentWithQuoteDetected(name)
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

			// Remove redundant array typecasts on ARRAY constructors
			// PostgreSQL normalizes ARRAY['a'::varchar]::text[] to ARRAY['a'::varchar::text]
			// by pushing down the array typecast to each element. Since we strip ::text casts
			// on elements, we also need to strip ::text[] on the ARRAY constructor itself.
			// e.g., ARRAY['a'::varchar]::text[] -> ARRAY['a'::varchar] (after stripping ::text[])
			//       ARRAY['a'::varchar::text]   -> ARRAY['a'::varchar] (after stripping ::text on element)
			// Empty arrays (ARRAY[]) need the typecast or PostgreSQL can't determine the type.
			normalizedExpr := normalizeCheckExpr(e.Expr, mode)
			if arrayConstructor, isArrayConstructor := normalizedExpr.(*parser.ArrayConstructor); isArrayConstructor {
				if strings.HasSuffix(typeStr, "[]") && len(arrayConstructor.Elements) > 0 {
					// Non-empty array with array typecast: strip the redundant typecast
					return normalizedExpr
				}
			}

			// For time types, keep the cast but use normalized type name
			return &parser.CastExpr{
				Expr: normalizedExpr,
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
			funcName := strings.ToLower(funcExpr.Name.Name)
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

		// For ANY/ALL expressions, normalize the array elements
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
		funcName := parser.NewIdent(strings.ToLower(e.Name.Name), false)
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
		// PostgreSQL normalizes BETWEEN to >= AND <=
		// e.g., "score BETWEEN 0 AND 100" becomes "score >= 0 AND score <= 100"
		if mode == GeneratorModePostgres {
			left := normalizeCheckExpr(e.Left, mode)
			from := normalizeCheckExpr(e.From, mode)
			to := normalizeCheckExpr(e.To, mode)

			if e.Operator == parser.BetweenStr {
				// x BETWEEN a AND b -> x >= a AND x <= b
				return &parser.AndExpr{
					Left:  &parser.ComparisonExpr{Left: left, Operator: parser.GreaterEqualStr, Right: from},
					Right: &parser.ComparisonExpr{Left: left, Operator: parser.LessEqualStr, Right: to},
				}
			} else if e.Operator == parser.NotBetweenStr {
				// x NOT BETWEEN a AND b -> x < a OR x > b
				return &parser.OrExpr{
					Left:  &parser.ComparisonExpr{Left: left, Operator: parser.LessThanStr, Right: from},
					Right: &parser.ComparisonExpr{Left: left, Operator: parser.GreaterThanStr, Right: to},
				}
			}
		}
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
		// Normalize column names while preserving quoting information:
		// - Quoted identifiers that are NOT all lowercase preserve their case and remain quoted
		// - Quoted identifiers that ARE all lowercase are normalized to unquoted (since "id" = id)
		// - Unquoted identifiers are normalized to lowercase
		var qualifier Ident
		if !e.Qualifier.Name.IsEmpty() {
			qualifier = NewNormalizedIdent(e.Qualifier.Name)
		}
		return &parser.ColName{
			Name:      NewNormalizedIdent(e.Name),
			Qualifier: parser.TableName{Name: qualifier},
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
		// Normalize column name and qualifier while preserving quoting:
		// - Quoted identifiers that are NOT all lowercase preserve their case and remain quoted
		// - Quoted identifiers that ARE all lowercase are normalized to unquoted (since "id" = id)
		// - Unquoted identifiers are normalized to lowercase
		// For Postgres and MySQL, remove table qualifiers (e.g., "users.name" -> "name")
		var qualifier Ident
		if !e.Qualifier.Name.IsEmpty() {
			// For Postgres and MySQL, remove table qualifiers
			if mode != GeneratorModePostgres && mode != GeneratorModeMysql {
				qualifier = NewNormalizedIdent(e.Qualifier.Name)
			}
		}
		return &parser.ColName{
			Name:      NewNormalizedIdent(e.Name),
			Qualifier: parser.TableName{Name: qualifier},
		}
	case *parser.ArrayConstructor:
		elements := util.TransformSlice(e.Elements, func(elem parser.Expr) parser.Expr {
			return normalizeExpr(elem, mode)
		})
		return &parser.ArrayConstructor{Elements: elements}
	case *parser.FuncExpr:
		funcName := strings.ToLower(e.Name.Name)
		// For PostgreSQL, normalize date/time function calls to keywords
		// The generic parser parses CURRENT_DATE in parentheses as a function call,
		// but without parentheses as a keyword (SQLVal with ValArg type)
		// e.g., (CURRENT_DATE) -> current_date(), but CURRENT_DATE -> current_date
		if mode == GeneratorModePostgres && len(e.Exprs) == 0 {
			switch funcName {
			case "current_date", "current_time", "current_timestamp":
				return parser.NewValArg(funcName)
			}
		}
		normalizedExprs := parser.SelectExprs{}
		for _, arg := range e.Exprs {
			// For Postgres, check for ARRAY constructors BEFORE normalizing
			// PostgreSQL standardizes function arguments to use ARRAY['a', 'b'] notation
			// but users may write them expanded as 'a', 'b', so we expand for comparison
			// e.g., jsonb_extract_path_text(payload, ARRAY['amount']) -> jsonb_extract_path_text(payload, 'amount')
			// e.g., jsonb_extract_path_text(payload, ARRAY['a', 'b']) -> jsonb_extract_path_text(payload, 'a', 'b')
			// However, do NOT expand for ANY/ALL/SOME functions as they require the ARRAY constructor
			if mode == GeneratorModePostgres && funcName != "any" && funcName != "all" && funcName != "some" {
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
		// Normalize function name to lowercase for PostgreSQL (PostgreSQL stores functions in lowercase)
		// For MySQL, preserve the original case as MySQL preserves case for function names
		normalizedName := e.Name
		if mode == GeneratorModePostgres {
			normalizedName = parser.NewIdent(funcName, e.Name.Quoted)
		}
		return &parser.FuncExpr{
			Qualifier: e.Qualifier,
			Name:      normalizedName,
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
				if sqlVal, ok := normalizedExpr.(*parser.SQLVal); ok {
					// Handle string literals
					if sqlVal.Type == parser.StrVal {
						// PostgreSQL stores negative numbers as string literals with casts like '-20'::integer
						// We convert these back to plain numeric literals
						switch typeStr {
						case "integer", "bigint", "smallint":
							// Convert numeric string to actual numeric literal
							// This unwraps '-20'::integer -> -20
							return parser.NewIntVal(sqlVal.Val)
						case "numeric", "decimal", "real", "double precision":
							return parser.NewFloatVal(sqlVal.Val)
						case "text", "character varying":
							// Strip redundant text casts on string literals
							return normalizedExpr
						case "date", "time", "timestamp", "timestamp without time zone":
							// Strip date/time casts on literals (PostgreSQL adds these for typed literals)
							return normalizedExpr
						default:
							slog.Debug("unhandled cast type in normalizeCastExpr", "type", typeStr)
						}
					} else if sqlVal.Type == parser.IntVal || sqlVal.Type == parser.FloatVal {
						// Handle numeric literals that already have explicit types
						// PostgreSQL adds redundant casts like 100::numeric or 3.14::double precision
						// Strip these redundant casts when casting to numeric types
						switch typeStr {
						case "integer", "bigint", "smallint", "numeric", "decimal", "real", "double precision":
							// The value is already a numeric literal, no cast needed
							return normalizedExpr
						}
					}
				}
				// Strip redundant casts on NULL values and normalize to lowercase
				// PostgreSQL stores DEFAULT NULL as NULL::type, but we normalize to just null
				// (matching the lexer's keyword normalization to lowercase)
				if sqlVal, ok := normalizedExpr.(*parser.SQLVal); ok && sqlVal.Type == parser.ValArg {
					if strings.EqualFold(sqlVal.Val, "null") {
						// Strip all type casts on NULL and return lowercase null (matching lexer)
						return parser.NewValArg("null")
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
				// HOWEVER: Empty arrays (ARRAY[]) NEED the typecast or PostgreSQL can't determine the type
				if arrayConstructor, isArrayConstructor := normalizedExpr.(*parser.ArrayConstructor); isArrayConstructor {
					// Check if this is an array type cast (type string ends with [])
					if strings.HasSuffix(typeStr, "[]") {
						// This is an array type (e.g., text[], int[])
						// Only strip the typecast if the array is NOT empty
						if len(arrayConstructor.Elements) > 0 {
							// Non-empty array: strip the redundant typecast
							return normalizedExpr
						}
						// Empty array: preserve the typecast (ARRAY[]::int[] is required)
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
		// For PostgreSQL and MySQL, unwrap parentheses around most expressions to normalize
		// Both databases add parentheses around many expressions, but we want a canonical form
		// We always unwrap ParenExpr during normalization to get a canonical form
		// The only exception is when parentheses are around complex nested expressions
		// where they're needed for precedence (like CASE inside a larger expression)
		if mode == GeneratorModePostgres || mode == GeneratorModeMysql {
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
						return parser.NewIntVal("-" + sqlVal.Val)
					}
				case parser.FloatVal:
					// Create negative float: -N.M
					if sqlVal.Val[0] == '-' {
						// Double negative: --N.M → N.M
						return parser.NewFloatVal(sqlVal.Val[1:])
					} else {
						return parser.NewFloatVal("-" + sqlVal.Val)
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
		as := e.As
		// For PostgreSQL, strip automatic aliases like ?column?
		if mode == GeneratorModePostgres && as.Name == "?column?" {
			as = parser.NewIdent("", false)
		}
		// For MySQL, strip redundant aliases where the alias matches the column name
		// MySQL adds "column_name as column_name" which is redundant
		if mode == GeneratorModeMysql && !as.IsEmpty() {
			if colName, ok := e.Expr.(*parser.ColName); ok {
				if strings.EqualFold(colName.Name.Name, as.Name) {
					// The alias is the same as the column name, strip it
					as = parser.NewIdent("", false)
				}
			}
		}
		return &parser.AliasedExpr{
			Expr: normalizeExpr(e.Expr, mode),
			As:   as,
		}
	case *parser.StarExpr:
		return e
	default:
		return expr
	}
}

// normalizeTableExprs normalizes FROM clause table expressions
func normalizeTableExprs(exprs parser.TableExprs, mode GeneratorMode) parser.TableExprs {
	normalized := make(parser.TableExprs, len(exprs))
	for i, expr := range exprs {
		normalized[i] = normalizeTableExpr(expr, mode)
	}
	return normalized
}

// normalizeTableExpr normalizes a single TableExpr
func normalizeTableExpr(expr parser.TableExpr, mode GeneratorMode) parser.TableExpr {
	if expr == nil {
		return nil
	}

	switch e := expr.(type) {
	case *parser.AliasedTableExpr:
		normalizedExpr := e.Expr
		// For MySQL, normalize table names to remove database prefix
		// MySQL stores views with database.table references, but we want just table names
		if mode == GeneratorModeMysql {
			if tableName, ok := e.Expr.(parser.TableName); ok {
				// Remove the database/schema part, keep only the table name
				normalizedExpr = parser.TableName{
					Schema: Ident{}, // Remove schema/database
					Name:   tableName.Name,
				}
			}
		}
		return &parser.AliasedTableExpr{
			Expr:       normalizedExpr,
			Partitions: e.Partitions,
			As:         e.As,
			TableHints: e.TableHints,
			IndexHints: e.IndexHints,
		}
	case *parser.JoinTableExpr:
		return &parser.JoinTableExpr{
			LeftExpr:  normalizeTableExpr(e.LeftExpr, mode),
			Join:      e.Join,
			RightExpr: normalizeTableExpr(e.RightExpr, mode),
			Condition: normalizeJoinCondition(e.Condition, mode),
		}
	case *parser.ParenTableExpr:
		// PostgreSQL adds parentheses around JOINs when storing views
		// Unwrap these to get a canonical form
		if mode == GeneratorModePostgres && len(e.Exprs) == 1 {
			// Single expression in parentheses - unwrap it
			return normalizeTableExpr(e.Exprs[0], mode)
		}
		// Multiple expressions or non-Postgres mode - normalize but keep parens
		normalized := make(parser.TableExprs, len(e.Exprs))
		for i, expr := range e.Exprs {
			normalized[i] = normalizeTableExpr(expr, mode)
		}
		return &parser.ParenTableExpr{Exprs: normalized}
	default:
		return expr
	}
}

// normalizeJoinCondition normalizes the JOIN ON/USING condition
func normalizeJoinCondition(cond parser.JoinCondition, mode GeneratorMode) parser.JoinCondition {
	if cond.On != nil {
		// For PostgreSQL and MySQL, preserve table qualifiers in JOIN ON clauses
		// They're needed for disambiguation (e.g., "u.id = o.user_id")
		// We only normalize the expression structure (parentheses, etc.), not column qualifiers
		return parser.JoinCondition{
			On:    normalizeExprPreservingQualifiers(cond.On, mode),
			Using: cond.Using,
		}
	}
	return cond
}

// normalizeExprPreservingQualifiers is like normalizeExpr but preserves table qualifiers in column references
// This is used for JOIN ON clauses where qualifiers are semantically important
func normalizeExprPreservingQualifiers(expr parser.Expr, mode GeneratorMode) parser.Expr {
	if expr == nil {
		return nil
	}

	switch e := expr.(type) {
	case *parser.ColName:
		// Keep the qualifier but normalize the names to lowercase
		qualifierStr := ""
		if !e.Qualifier.Name.IsEmpty() {
			qualifierStr = normalizeName(e.Qualifier.Name.Name)
		}
		nameStr := normalizeName(e.Name.Name)

		return &parser.ColName{
			Name: parser.NewIdent(nameStr, false),
			Qualifier: parser.TableName{
				Name: parser.NewIdent(qualifierStr, false),
			},
		}
	case *parser.AndExpr:
		return &parser.AndExpr{
			Left:  normalizeExprPreservingQualifiers(e.Left, mode),
			Right: normalizeExprPreservingQualifiers(e.Right, mode),
		}
	case *parser.OrExpr:
		return &parser.OrExpr{
			Left:  normalizeExprPreservingQualifiers(e.Left, mode),
			Right: normalizeExprPreservingQualifiers(e.Right, mode),
		}
	case *parser.ComparisonExpr:
		return &parser.ComparisonExpr{
			Operator: normalizeOperator(e.Operator, mode),
			Left:     normalizeExprPreservingQualifiers(e.Left, mode),
			Right:    normalizeExprPreservingQualifiers(e.Right, mode),
			Escape:   normalizeExprPreservingQualifiers(e.Escape, mode),
		}
	case *parser.ParenExpr:
		normalizedInner := normalizeExprPreservingQualifiers(e.Expr, mode)
		// For PostgreSQL and MySQL, unwrap unnecessary parentheses
		if mode == GeneratorModePostgres || mode == GeneratorModeMysql {
			return normalizedInner
		}
		return &parser.ParenExpr{Expr: normalizedInner}
	default:
		// For other expressions, use the regular normalizeExpr
		return normalizeExpr(expr, mode)
	}
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
			Definition: normalizeViewDefinition(cte.Definition, mode, nil),
		}
	}

	return &parser.With{
		CTEs:      normalizedCTEs,
		Recursive: with.Recursive,
	}
}

// TableLookupFunc is a function type for looking up a table by name.
// Used by normalizeViewDefinition to expand SELECT * expressions.
type TableLookupFunc func(name QualifiedName) *Table

// normalizeViewDefinition normalizes a view definition AST for comparison.
// This function removes database-specific formatting differences that don't affect the logical meaning.
// If tableLookup is provided, SELECT * expressions are expanded to explicit column names
// (PostgreSQL expands * when storing view definitions).
func normalizeViewDefinition(stmt parser.SelectStatement, mode GeneratorMode, tableLookup TableLookupFunc) parser.SelectStatement {
	if stmt == nil {
		return nil
	}

	switch s := stmt.(type) {
	case *parser.Select:
		selectExprs := s.SelectExprs
		// Expand SELECT * if we have table lookup capability
		if tableLookup != nil && hasStarExpr(selectExprs) {
			if tableName := extractTableNameFromFrom(s.From); !tableName.IsEmpty() {
				if table := tableLookup(tableName); table != nil {
					selectExprs = expandStarExpr(selectExprs, table)
				}
			}
		}
		return &parser.Select{
			Cache:       s.Cache,
			Comments:    nil, // Remove comments for view comparison - they don't affect semantic meaning
			Distinct:    s.Distinct,
			Hints:       s.Hints,
			SelectExprs: normalizeSelectExprs(selectExprs, mode),
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
			Left:    normalizeViewDefinition(s.Left, mode, tableLookup),
			Right:   normalizeViewDefinition(s.Right, mode, tableLookup),
			OrderBy: normalizeOrderBy(s.OrderBy, mode),
			Limit:   s.Limit,
			Lock:    s.Lock,
			With:    normalizeWith(s.With, mode),
		}
	default:
		return stmt
	}
}

// hasStarExpr checks if there's a StarExpr in the SELECT list.
func hasStarExpr(exprs parser.SelectExprs) bool {
	for _, expr := range exprs {
		if _, ok := expr.(*parser.StarExpr); ok {
			return true
		}
	}
	return false
}

// extractTableNameFromFrom extracts the table name from a simple FROM clause.
// Returns empty QualifiedName if the FROM clause is complex (joins, subqueries, etc.)
func extractTableNameFromFrom(from parser.TableExprs) QualifiedName {
	if len(from) != 1 {
		return QualifiedName{}
	}

	switch expr := from[0].(type) {
	case *parser.AliasedTableExpr:
		if tableName, ok := expr.Expr.(parser.TableName); ok {
			return QualifiedName{
				Schema: tableName.Schema,
				Name:   tableName.Name,
			}
		}
	}

	return QualifiedName{}
}

// getSortedColumns converts a column map to a slice sorted by position.
// This is necessary because Go maps have non-deterministic iteration order,
// but column order matters for:
// - DDL generation (columns should appear in their original declaration order)
// - SELECT * expansion (PostgreSQL expands * to columns in ordinal position order)
func getSortedColumns(columns map[string]*Column) []*Column {
	result := make([]*Column, 0, len(columns))
	for _, col := range columns {
		result = append(result, col)
	}
	slices.SortFunc(result, func(a, b *Column) int {
		return a.position - b.position
	})
	return result
}

// expandStarExpr replaces StarExpr with explicit column references.
func expandStarExpr(exprs parser.SelectExprs, table *Table) parser.SelectExprs {
	var result parser.SelectExprs

	for _, expr := range exprs {
		switch expr.(type) {
		case *parser.StarExpr:
			// Get columns sorted by position to match PostgreSQL's expansion order
			columns := getSortedColumns(table.columns)
			for _, col := range columns {
				colRef := &parser.AliasedExpr{
					Expr: &parser.ColName{
						Name: col.name,
					},
				}
				result = append(result, colRef)
			}
		default:
			result = append(result, expr)
		}
	}

	return result
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
func sortAndDeduplicateValues[T parser.Expr](values []T) []T {
	if len(values) <= 1 {
		return values
	}

	slices.SortFunc(values, func(a, b T) int {
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

// normalizeCommentObject returns a normalized object path for a COMMENT statement.
// For PostgreSQL, this prepends the default schema if missing:
//   - OBJECT_TABLE: [table] -> [schema, table]
//   - OBJECT_COLUMN: [table, column] -> [schema, table, column]
//   - INDEX/VIEW/TYPE/DOMAIN/FUNCTION: [name] -> [schema, name]
//   - CONSTRAINT/TRIGGER: [name, table] -> [name, schema, table]
//
// For other databases or when schema is already present, returns the original object.
func normalizeCommentObject(comment *parser.Comment, mode GeneratorMode, defaultSchema string) []Ident {
	if mode != GeneratorModePostgres || defaultSchema == "" {
		return comment.Object
	}

	var needsSchema bool
	switch comment.ObjectType {
	case "OBJECT_TABLE", "OBJECT_INDEX", "OBJECT_VIEW", "OBJECT_TYPE", "OBJECT_DOMAIN", "OBJECT_FUNCTION":
		// These types need [schema, name]
		needsSchema = len(comment.Object) == 1
	case "OBJECT_COLUMN":
		// COLUMN comments need [schema, table, column]
		needsSchema = len(comment.Object) == 2
	case "OBJECT_CONSTRAINT", "OBJECT_TRIGGER":
		// These types need [name, schema, table] - schema is inserted at position 1
		needsSchema = len(comment.Object) == 2
		if needsSchema {
			schemaIdent := parser.NewIdent(defaultSchema, false)
			return []Ident{comment.Object[0], schemaIdent, comment.Object[1]}
		}
		return comment.Object
	default:
		panic("unexpected comment object type: " + comment.ObjectType)
	}

	if !needsSchema {
		return comment.Object
	}

	// Prepend default schema (unquoted)
	schemaIdent := parser.NewIdent(defaultSchema, false)
	return append([]Ident{schemaIdent}, comment.Object...)
}

func isPostgresSerialType(typeName string) bool {
	_, ok := postgresSerialTypes[strings.ToLower(typeName)]
	return ok
}

func getSerialUnderlyingType(typeName string) string {
	return postgresSerialTypes[strings.ToLower(typeName)]
}

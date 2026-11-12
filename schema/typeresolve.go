package schema

import (
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/sqldef/sqldef/v3/parser"
	"github.com/sqldef/sqldef/v3/schema/pgcatalog"
	"github.com/sqldef/sqldef/v3/util"
)

// PostgreSQL stores an expression with the casts that resolving its types implies: it adds
// the implicit casts to the types a function or an operator takes, as in round((n)::numeric, 2)
// for round(n, 2) on an integer column, or (c)::integer > 0 for a domain column, and it drops a
// cast of a value to the type that it already has. The desired schema is written without them, so
// sqldef resolves the types of both sides the same way before comparing them.

// exprType is the type of an expression: a typname of pg_catalog or the key of a user-defined
// type, and its type modifier as written, such as "10" or "10,2". name is "" where the type is
// not known.
type exprType struct {
	name string
	mod  string
}

// typeContext resolves the types of the expressions of one schema
type typeContext struct {
	g        *Generator
	resolver *pgcatalog.Resolver
	// userFunctions are the folded names of the functions of the schema, which may overload or
	// shadow the built-in ones
	userFunctions map[string]bool
	tables        []*Table
	views         []*View
	viewOutputs   map[*View]*viewOutput
	// knownCasts are the casts whose operand and target types are both known
	knownCasts map[*parser.CastExpr]bool
	// depth counts the resolutions in progress, as resolving a view resolves the views it reads
	depth int
	// unresolved counts the expressions whose types are unknown
	unresolved int
	// ctes are the name keys of the common table expressions in scope of the query being
	// resolved, which its subqueries see
	ctes map[string]cteColumns
	// collatedDomains are the keys of the domains with a collation of their own or their base's
	collatedDomains map[string]bool
	// collated is set when an expression reads a column, a cast or a COLLATE clause with a
	// collation that may differ from the default of its type, which the types do not tell apart
	collated bool
}

func (g *Generator) newTypeContext(types []*Type, domains []*Domain, functions []*Function) *typeContext {
	tc := &typeContext{g: g, userFunctions: map[string]bool{}, knownCasts: map[*parser.CastExpr]bool{}, collatedDomains: map[string]bool{}}
	var userTypes []*pgcatalog.Type
	for _, t := range types {
		if len(t.enumValues) == 0 {
			continue
		}
		key := tc.userTypeKey(t.name)
		userTypes = append(userTypes,
			&pgcatalog.Type{Name: key, Category: pgcatalog.CategoryEnum, Kind: 'e'},
			&pgcatalog.Type{Name: key + "[]", Category: pgcatalog.CategoryArray, Kind: 'b', Elem: key})
	}
	// A domain over a domain of a later statement needs the resolver to know the latter first
	tc.resolver = pgcatalog.NewResolver(userTypes)
	known := 0
	for {
		var domainTypes []*pgcatalog.Type
		for _, domain := range domains {
			if base, ok := tc.domainBaseType(domain); ok {
				domainTypes = append(domainTypes, &pgcatalog.Type{Name: tc.userTypeKey(domain.name), Kind: 'd', Base: base})
			}
		}
		if len(domainTypes) == known {
			break
		}
		known = len(domainTypes)
		tc.resolver = pgcatalog.NewResolver(append(append([]*pgcatalog.Type{}, userTypes...), domainTypes...))
	}
	for changed := true; changed; {
		changed = false
		for _, domain := range domains {
			key := tc.userTypeKey(domain.name)
			base, _ := tc.domainBaseType(domain)
			if !tc.collatedDomains[key] && (domain.collation != "" || tc.collatedDomains[base]) {
				tc.collatedDomains[key] = true
				changed = true
			}
		}
	}
	for _, function := range functions {
		tc.userFunctions[tc.functionKey(function.name.Name)] = true
	}
	return tc
}

func (tc *typeContext) userTypeKey(name QualifiedName) string {
	return normalizeNameKey(name, tc.g.defaultSchema, tc.g.mode, tc.g.legacyIgnoreQuotes, 0)
}

func (tc *typeContext) functionKey(name Ident) string {
	if name.Quoted {
		return name.Name
	}
	return parser.FoldASCII(name.Name)
}

// typeOfName returns the type of a type name written in SQL, such as integer, "Mood" or
// public.mood[]
func (tc *typeContext) typeOfName(name string) (string, bool) {
	array := false
	for {
		trimmed, ok := strings.CutSuffix(strings.TrimSpace(name), "[]")
		if !ok {
			break
		}
		name, array = trimmed, true
	}
	parts, ok := parser.SplitPostgresName(name)
	if !ok || len(parts) == 0 || len(parts) > 2 {
		return "", false
	}
	var typ string
	if len(parts) == 1 || parser.IsPgCatalog(parts[0]) {
		last := parts[len(parts)-1]
		if last.Quoted {
			if tc.resolver.Type(last.Name) != nil && last.Name == parser.FoldASCII(last.Name) {
				typ = last.Name
			}
		} else if builtin, ok := pgcatalog.BuiltinTypeName(last.Name); ok {
			typ = builtin
		}
	}
	if typ == "" {
		qualified := QualifiedName{Name: parts[len(parts)-1]}
		if len(parts) == 2 {
			qualified.Schema = parts[0]
		}
		typ = tc.userTypeKey(qualified)
		if tc.resolver.Type(typ) == nil {
			return "", false
		}
	}
	if array {
		if typ = tc.resolver.ArrayType(typ); typ == "" {
			return "", false
		}
	}
	return typ, true
}

// castType returns the type of a cast
func (tc *typeContext) castType(t *parser.ConvertType) (exprType, bool) {
	// The grammar puts the [] of an array before the time zone
	base, array := strings.CutSuffix(t.Type, "[]")
	name := base + t.TimeZone
	if array || bool(t.Array) {
		name += "[]"
	}
	typ, ok := tc.typeOfName(name)
	if !ok {
		return exprType{}, false
	}
	if tc.collatedDomains[typ] {
		tc.collated = true
	}
	var length, scale string
	if t.Length != nil {
		length = t.Length.Val
	} else {
		length = defaultLength(strings.ToLower(t.Type))
	}
	if t.Scale != nil {
		scale = t.Scale.Val
	}
	return exprType{name: typ, mod: typeModifier(typ, length, scale)}, true
}

// columnType returns the type of a column
func (tc *typeContext) columnType(column *Column) (exprType, bool) {
	var typ string
	name := strings.ToLower(column.typeName)
	builtin, isBuiltin := pgcatalog.BuiltinTypeName(name)
	if column.references.Name == "" && !column.typeIdent.Quoted && isBuiltin {
		typ = builtin
		if column.timezone {
			switch typ {
			case "timestamp":
				typ = "timestamptz"
			case "time":
				typ = "timetz"
			}
		}
	} else {
		typ = tc.userTypeKey(columnTypeName(*column))
		if tc.resolver.Type(typ) == nil {
			return exprType{}, false
		}
	}
	var length, scale string
	if l := postgresTypeLength(*column); l != nil {
		length = l.raw
	}
	if column.scale != nil {
		scale = column.scale.raw
	}
	mod := typeModifier(typ, length, scale)
	if column.array {
		if typ = tc.resolver.ArrayType(typ); typ == "" {
			return exprType{}, false
		}
	}
	return exprType{name: typ, mod: mod}, true
}

// defaultLength returns the length of a type written without one: character and bit, as
// keywords, mean character(1) and bit(1), while bpchar and "bit" have no length
func defaultLength(written string) string {
	switch written {
	case "char", "character", "nchar", "national character", "bit":
		return "1"
	}
	return ""
}

// typeModifier returns the type modifier of a length and a scale, the same however it was written
func typeModifier(typ, length, scale string) string {
	if length == "" {
		return ""
	}
	if typ == "numeric" && scale == "" {
		scale = "0"
	}
	if scale == "" {
		return strings.TrimSpace(length)
	}
	return strings.TrimSpace(length) + "," + strings.TrimSpace(scale)
}

// columnScope returns the type of a column that an expression reads, or false
type columnScope func(col *parser.ColName) (exprType, bool)

// tableScope resolves the columns of a table
func (tc *typeContext) tableScope(table *Table) columnScope {
	return func(col *parser.ColName) (exprType, bool) {
		if !col.Qualifier.Name.IsEmpty() {
			if !tc.g.identsEqual(col.Qualifier.Name, table.name.Name) {
				return exprType{}, false
			}
			if !col.Qualifier.Schema.IsEmpty() && !tc.g.qualifiedNamesEqual(QualifiedName{Schema: col.Qualifier.Schema, Name: col.Qualifier.Name}, table.name) {
				return exprType{}, false
			}
		}
		for _, column := range table.columns {
			if tc.g.identsEqual(column.name, col.Name) {
				return tc.columnType(column)
			}
		}
		return exprType{}, false
	}
}

// typedForms are the forms of an expression with the casts that PostgreSQL adds or drops while
// resolving its types. plain leaves the casts to the comparison, which ignores the casts that
// PostgreSQL may add, as it did before sqldef resolved types. marked keeps the casts that it
// found PostgreSQL keeps, which takes the types of the whole expression to be known: full.
type typedForms[T any] struct {
	plain, marked T
	full          bool
}

// resolveForms resolves the types of an expression, or of a query, in both forms
func resolveForms[T any](tc *typeContext, resolve func() T) typedForms[T] {
	unresolved := tc.unresolved
	tc.depth++
	defer func() {
		tc.unresolved = unresolved
		// The casts are needed only while the trees that hold them are built
		if tc.depth--; tc.depth == 0 {
			clear(tc.knownCasts)
		}
	}()
	var forms typedForms[T]
	tc.unresolved = 0
	forms.marked = resolve()
	forms.full = tc.unresolved == 0
	// The forms differ only in the casts marked as resolved
	forms.plain = withoutResolvedCasts(forms.marked)
	return forms
}

// withoutResolvedCasts returns a copy of a tree whose casts are not marked as resolved
func withoutResolvedCasts[T any](tree T) T {
	castType := reflect.TypeFor[*parser.CastExpr]()
	var unmark func(v reflect.Value) (reflect.Value, bool)
	unmark = func(v reflect.Value) (reflect.Value, bool) {
		if v.Type() != castType || v.IsNil() {
			return reflect.Value{}, false
		}
		copied := reflect.New(castType.Elem())
		copied.Elem().Set(copyTree(v.Elem(), unmark))
		copied.Interface().(*parser.CastExpr).Resolved = false
		return copied, true
	}
	value := reflect.ValueOf(&tree).Elem()
	value.Set(copyTree(value, unmark))
	return tree
}

// typedPair returns the forms of two expressions to compare: the marked ones where both are known
func typedPair[T any](a, b typedForms[T]) (T, T) {
	if a.full && b.full {
		return a.marked, b.marked
	}
	return a.plain, b.plain
}

var boolType = exprType{name: "bool"}

func (tc *typeContext) resolve(expr parser.Expr, scope columnScope) (parser.Expr, exprType) {
	resolved, t := tc.resolveNode(expr, scope)
	if t.name == "" {
		tc.unresolved++
	}
	return resolved, t
}

func (tc *typeContext) resolveNode(expr parser.Expr, scope columnScope) (parser.Expr, exprType) {
	switch e := expr.(type) {
	case *parser.ParenExpr:
		inner, t := tc.resolve(e.Expr, scope)
		return &parser.ParenExpr{Expr: inner}, t
	case *parser.ColName:
		if t, ok := scope(e); ok {
			return e, t
		}
		return e, exprType{}
	case *parser.SQLVal:
		return e, tc.literalType(e)
	case *parser.NullVal:
		return e, exprType{name: pgcatalog.Unknown}
	case *parser.TypedLiteral:
		if t, ok := tc.typeOfName(e.Type); ok {
			return e, exprType{name: t}
		}
		return e, exprType{}
	case *parser.IntervalExpr:
		return e, exprType{name: "interval"}
	case *parser.ConvertExpr:
		// CAST(x AS t) is x::t
		if e.Action != parser.CastStr || e.Style != nil {
			return expr, exprType{}
		}
		return tc.resolveNode(&parser.CastExpr{Expr: e.Expr, Type: e.Type}, scope)
	case *parser.CastExpr:
		if array, ok := e.Expr.(*parser.ArrayConstructor); ok && strings.HasSuffix(e.Type.Type, "[]") {
			if to, ok := tc.castType(e.Type); ok {
				// PostgreSQL casts the elements of ARRAY[...]::t[] to t, each but those of t already
				elementType := *e.Type
				elementType.Type = strings.TrimSuffix(e.Type.Type, "[]")
				resolved := &parser.ArrayConstructor{Elements: util.TransformSlice(array.Elements, func(element parser.Expr) parser.Expr {
					cast := &parser.CastExpr{Expr: element, Type: &elementType}
					if _, nested := element.(*parser.ArrayConstructor); nested {
						cast.Type = e.Type
					}
					resolved, _ := tc.resolve(cast, scope)
					return resolved
				})}
				return resolved, to
			}
		}
		inner, from := tc.resolve(e.Expr, scope)
		to, ok := tc.castType(e.Type)
		if !ok {
			return &parser.CastExpr{Expr: inner, Type: e.Type}, exprType{}
		}
		if from == to {
			return inner, from
		}
		cast := &parser.CastExpr{Expr: inner, Type: e.Type}
		if from.name != "" {
			// PostgreSQL converts the operand of a cast with the cast alone
			cast.Expr = tc.markResolved(inner)
			tc.knownCasts[cast] = true
		}
		return cast, to
	case *parser.CollateExpr:
		tc.collated = true
		inner, t := tc.resolve(e.Expr, scope)
		return &parser.CollateExpr{Expr: inner, Charset: e.Charset}, t
	case *parser.AndExpr:
		left, _ := tc.resolve(e.Left, scope)
		right, _ := tc.resolve(e.Right, scope)
		return &parser.AndExpr{Left: left, Right: right}, boolType
	case *parser.OrExpr:
		left, _ := tc.resolve(e.Left, scope)
		right, _ := tc.resolve(e.Right, scope)
		return &parser.OrExpr{Left: left, Right: right}, boolType
	case *parser.NotExpr:
		inner, _ := tc.resolve(e.Expr, scope)
		return &parser.NotExpr{Expr: inner}, boolType
	case *parser.IsExpr:
		inner, _ := tc.resolve(e.Expr, scope)
		return &parser.IsExpr{Operator: e.Operator, Expr: inner}, boolType
	case *parser.ComparisonExpr:
		left, lt := tc.resolve(e.Left, scope)
		right, rt := tc.resolve(e.Right, scope)
		resolved := &parser.ComparisonExpr{Operator: e.Operator, Left: left, Right: right, Escape: e.Escape, All: e.All, Any: e.Any}
		if e.Escape != nil || e.Any || e.All {
			return resolved, boolType
		}
		if op, ok := comparisonOperators[e.Operator]; ok {
			if args, t, ok := tc.resolveOperator(op, []parser.Expr{left, right}, []exprType{lt, rt}); ok {
				resolved.Left, resolved.Right = args[0], args[1]
				return resolved, t
			}
			// The comparison is still boolean, but the casts of its operands are unknown
			tc.unresolved++
		}
		return resolved, boolType
	case *parser.BinaryExpr:
		left, lt := tc.resolve(e.Left, scope)
		right, rt := tc.resolve(e.Right, scope)
		resolved := &parser.BinaryExpr{Operator: e.Operator, Left: left, Right: right}
		args, t, ok := tc.resolveOperator(e.Operator, []parser.Expr{left, right}, []exprType{lt, rt})
		if !ok {
			return resolved, exprType{}
		}
		resolved.Left, resolved.Right = args[0], args[1]
		return resolved, t
	case *parser.ConcatExpr:
		left, lt := tc.resolve(e.Left, scope)
		right, rt := tc.resolve(e.Right, scope)
		args, t, ok := tc.resolveOperator("||", []parser.Expr{left, right}, []exprType{lt, rt})
		if !ok {
			return &parser.ConcatExpr{Left: left, Right: right}, exprType{}
		}
		return &parser.ConcatExpr{Left: args[0], Right: args[1]}, t
	case *parser.UnaryExpr:
		inner, t := tc.resolve(e.Expr, scope)
		args, rt, ok := tc.resolveOperator(e.Operator, []parser.Expr{inner}, []exprType{t})
		if !ok {
			return &parser.UnaryExpr{Operator: e.Operator, Expr: inner}, exprType{}
		}
		return &parser.UnaryExpr{Operator: e.Operator, Expr: args[0]}, rt
	case *parser.FuncExpr:
		return tc.resolveFunc(e, scope)
	case *parser.Subquery:
		return tc.resolveSubquery(e, scope)
	case *parser.ExistsExpr:
		subquery, _ := tc.resolveSubquery(e.Subquery, scope)
		return &parser.ExistsExpr{Subquery: subquery.(*parser.Subquery)}, boolType
	case *parser.CaseExpr:
		return tc.resolveCase(e, scope)
	default:
		return expr, exprType{}
	}
}

// comparisonOperators are the operators of the comparisons that PostgreSQL resolves like
// any other operator
var comparisonOperators = map[string]string{
	parser.EqualStr:           "=",
	parser.LessThanStr:        "<",
	parser.GreaterThanStr:     ">",
	parser.LessEqualStr:       "<=",
	parser.GreaterEqualStr:    ">=",
	parser.NotEqualStr:        "<>",
	"<>":                      "<>",
	parser.LikeStr:            "~~",
	parser.NotLikeStr:         "!~~",
	parser.ILikeStr:           "~~*",
	parser.NotILikeStr:        "!~~*",
	parser.PosixRegexStr:      "~",
	parser.PosixRegexCiStr:    "~*",
	parser.PosixNotRegexStr:   "!~",
	parser.PosixNotRegexCiStr: "!~*",
}

// literalType returns the type that PostgreSQL gives a constant
func (tc *typeContext) literalType(val *parser.SQLVal) exprType {
	switch val.Type {
	case parser.StrVal:
		if val.DroppedCast == nil {
			return exprType{name: pgcatalog.Unknown}
		}
		if t, ok := tc.castType(val.DroppedCast); ok {
			return t
		}
	case parser.IntVal:
		if _, err := strconv.ParseInt(val.Val, 10, 32); err == nil {
			return exprType{name: "int4"}
		}
		if _, err := strconv.ParseInt(val.Val, 10, 64); err == nil {
			return exprType{name: "int8"}
		}
		return exprType{name: "numeric"}
	case parser.FloatVal:
		return exprType{name: "numeric"}
	case parser.ValBool:
		return boolType
	case parser.BitVal:
		return exprType{name: "bit"}
	}
	return exprType{}
}

// resolveOperator chooses the operator for the types of its arguments, and returns the arguments
// with the casts to the types that it takes. A prefix operator has one argument.
func (tc *typeContext) resolveOperator(name string, args []parser.Expr, types []exprType) ([]parser.Expr, exprType, bool) {
	if !tc.allKnown(types) {
		return nil, exprType{}, false
	}
	var call *pgcatalog.Call
	var ok bool
	if len(args) == 1 {
		call, ok = tc.resolver.ResolveOperator(name, "", types[0].name)
	} else {
		call, ok = tc.resolver.ResolveOperator(name, types[0].name, types[1].name)
	}
	if !ok {
		return nil, exprType{}, false
	}
	return tc.withCasts(args, types, call.Params), exprType{name: call.Result}, true
}

func (tc *typeContext) allKnown(types []exprType) bool {
	for _, t := range types {
		if t.name == "" {
			return false
		}
	}
	return true
}

// withCasts returns the arguments with casts to the types of the parameters that they differ from.
// PostgreSQL gives an unknown constant the type of its parameter without a cast that it writes
// back in every case, which the comparison of literals already takes care of.
func (tc *typeContext) withCasts(args []parser.Expr, types []exprType, params []string) []parser.Expr {
	result := make([]parser.Expr, len(args))
	for i, arg := range args {
		result[i] = tc.markResolved(arg)
		if types[i].name == pgcatalog.Unknown || types[i].name == params[i] || params[i] == "any" || params[i] == pgcatalog.Unknown {
			continue
		}
		if t := tc.resolver.Type(params[i]); t != nil && t.Display != "" {
			result[i] = &parser.CastExpr{Expr: result[i], Type: &parser.ConvertType{Type: t.Display}, Resolved: true}
		}
	}
	return result
}

// markResolved marks a cast in a place where PostgreSQL adds none of its own, so that the
// comparison keeps it, once its operand's type is known
func (tc *typeContext) markResolved(expr parser.Expr) parser.Expr {
	inner := expr
	for {
		paren, ok := inner.(*parser.ParenExpr)
		if !ok {
			break
		}
		inner = paren.Expr
	}
	if cast, ok := inner.(*parser.CastExpr); ok && tc.knownCasts[cast] {
		cast.Resolved = true
	}
	return expr
}

func (tc *typeContext) resolveFunc(e *parser.FuncExpr, scope columnScope) (parser.Expr, exprType) {
	resolved := *e
	resolved.Exprs = make(parser.SelectExprs, len(e.Exprs))
	args := make([]parser.Expr, 0, len(e.Exprs))
	types := make([]exprType, 0, len(e.Exprs))
	plain := true
	for i, selectExpr := range e.Exprs {
		aliased, ok := selectExpr.(*parser.AliasedExpr)
		if !ok || !aliased.As.IsEmpty() {
			resolved.Exprs[i] = selectExpr
			// count(*) is count() in pg_catalog
			_, isStar := selectExpr.(*parser.StarExpr)
			plain = plain && isStar && len(e.Exprs) == 1
			continue
		}
		arg, t := tc.resolve(aliased.Expr, scope)
		resolved.Exprs[i] = &parser.AliasedExpr{Expr: arg}
		args = append(args, arg)
		types = append(types, t)
	}
	// The ORDER BY of an ordered-set aggregate gives the arguments after the direct ones
	direct := len(args)
	if e.WithinGroup != nil {
		resolved.WithinGroup = make(parser.OrderBy, len(e.WithinGroup))
		for i, order := range e.WithinGroup {
			arg, t := tc.resolve(order.Expr, scope)
			resolved.WithinGroup[i] = &parser.Order{Expr: arg, Direction: order.Direction}
			args = append(args, arg)
			types = append(types, t)
		}
	}
	if !plain || !tc.allKnown(types) {
		return &resolved, exprType{}
	}
	if !e.Qualifier.IsEmpty() && !parser.IsPgCatalog(e.Qualifier) {
		return &resolved, exprType{}
	}
	name := tc.functionKey(e.Name)
	if tc.userFunctions[name] {
		return &resolved, exprType{}
	}

	var params []string
	var result exprType
	switch name {
	case "coalesce", "greatest", "least":
		common, ok := tc.resolver.CommonType(typeNames(types))
		if !ok {
			return &resolved, exprType{}
		}
		params = make([]string, len(types))
		for i := range params {
			params[i] = common
		}
		result = commonType(types, common)
	case "nullif":
		if len(types) != 2 {
			return &resolved, exprType{}
		}
		call, ok := tc.resolver.ResolveOperator("=", types[0].name, types[1].name)
		if !ok {
			return &resolved, exprType{}
		}
		// The result has the type of the first argument converted for the operator
		params, result = call.Params, exprType{name: call.Params[0]}
		if result.name == types[0].name {
			result = types[0]
		}
	default:
		// A function named like a type is a cast where no function takes the argument
		if _, isType := pgcatalog.BuiltinTypeName(name); isType {
			return &resolved, exprType{}
		}
		call, ok := tc.resolver.ResolveFunction(name, typeNames(types))
		if !ok {
			return &resolved, exprType{}
		}
		params, result = call.Params, exprType{name: call.Result}
		// A hypothetical-set aggregate converts each direct argument and its aggregated
		// argument to their common type
		if e.WithinGroup != nil && direct*2 == len(args) && slices.Equal(slices.Compact(slices.Clone(params)), []string{"any"}) {
			for i := range direct {
				common, ok := tc.resolver.CommonType([]string{types[i].name, types[direct+i].name})
				if !ok {
					return &resolved, exprType{}
				}
				params[i], params[direct+i] = common, common
			}
		}
	}
	cast := tc.withCasts(args, types, params)
	j := 0
	for i, selectExpr := range resolved.Exprs {
		if aliased, ok := selectExpr.(*parser.AliasedExpr); ok && aliased.As.IsEmpty() {
			resolved.Exprs[i] = &parser.AliasedExpr{Expr: cast[j]}
			j++
		}
	}
	for i, order := range resolved.WithinGroup {
		order.Expr = cast[direct+i]
	}
	return &resolved, result
}

// commonType returns the common type of values with the type modifier they all have, as
// select_common_typmod() does
func commonType(types []exprType, common string) exprType {
	for _, t := range types {
		if t.name != common || t.mod != types[0].mod {
			return exprType{name: common}
		}
	}
	return exprType{name: common, mod: types[0].mod}
}

func typeNames(types []exprType) []string {
	names := make([]string, len(types))
	for i, t := range types {
		names[i] = t.name
	}
	return names
}

// resolveSubquery resolves a subquery, which reads the columns of the enclosing queries too. A
// scalar subquery has the type of its one column.
func (tc *typeContext) resolveSubquery(e *parser.Subquery, scope columnScope) (parser.Expr, exprType) {
	resolved := &parser.Subquery{Select: tc.resolveSelect(e.Select, scope, tc.ctes)}
	columns, ok := tc.outputColumns(e.Select, resolved.Select, false)
	if !ok || len(columns) != 1 {
		return resolved, exprType{}
	}
	return resolved, columns[0].typ
}

// resolveCase converts the results of a CASE to their common type
func (tc *typeContext) resolveCase(e *parser.CaseExpr, scope columnScope) (parser.Expr, exprType) {
	resolved := &parser.CaseExpr{Whens: make([]*parser.When, len(e.Whens))}
	if e.Expr != nil {
		resolved.Expr, _ = tc.resolve(e.Expr, scope)
	}
	var results []parser.Expr
	var types []exprType
	for i, when := range e.Whens {
		cond, _ := tc.resolve(when.Cond, scope)
		val, t := tc.resolve(when.Val, scope)
		resolved.Whens[i] = &parser.When{Cond: cond, Val: val}
		results = append(results, val)
		types = append(types, t)
	}
	if e.Else != nil {
		val, t := tc.resolve(e.Else, scope)
		resolved.Else = val
		results = append(results, val)
		types = append(types, t)
	}
	if !tc.allKnown(types) {
		return resolved, exprType{}
	}
	common, ok := tc.resolver.CommonType(typeNames(types))
	if !ok {
		return resolved, exprType{}
	}
	params := make([]string, len(types))
	for i := range params {
		params[i] = common
	}
	cast := tc.withCasts(results, types, params)
	for i := range resolved.Whens {
		resolved.Whens[i].Val = cast[i]
	}
	if resolved.Else != nil {
		resolved.Else = cast[len(cast)-1]
	}
	return resolved, commonType(types, common)
}

// resolveExpressionTypes resolves the types of the expressions of the tables and views of both
// schemas, which the comparisons then take through typed and typedView
func (g *Generator) resolveExpressionTypes() {
	g.typedExprs = map[parser.Expr]typedForms[parser.Expr]{}
	g.typedViewDefinitions = map[parser.SelectStatement]typedForms[parser.SelectStatement]{}
	g.exprTypes = map[parser.Expr]exprType{}
	g.viewColumnTypes = map[parser.SelectStatement][]relationColumn{}
	desired := g.newTypeContext(g.desiredTypes, g.desiredDomains, g.desiredFunctions)
	current := g.newTypeContext(g.currentTypes, g.currentDomains, g.currentFunctions)
	desired.setRelations(g.desiredTables, g.desiredViews)
	current.setRelations(g.currentTables, g.currentViews)
	for _, tc := range []*typeContext{desired, current} {
		for _, table := range tc.tables {
			g.resolveTableExpressionTypes(tc, table)
		}
		domains := g.desiredDomains
		if tc == current {
			domains = g.currentDomains
		}
		for _, domain := range domains {
			g.resolveDomainExpressionTypes(tc, domain)
		}
		for _, view := range tc.views {
			tc.viewColumns(view)
		}
	}
}

func (g *Generator) resolveTableExpressionTypes(tc *typeContext, table *Table) {
	scope := tc.tableScope(table)
	add := func(expr parser.Expr) {
		if expr != nil && hashableExpr(expr) {
			g.typedExprs[expr] = resolveForms(tc, func() parser.Expr {
				resolved, _ := tc.resolve(expr, scope)
				return resolved
			})
		}
	}
	for _, column := range util.CanonicalMapIter(table.columns) {
		if column.check != nil {
			add(column.check.definition)
		}
	}
	for _, check := range table.checks {
		add(check.definition)
	}
	for _, index := range table.indexes {
		for _, column := range index.columns {
			add(column.columnExpr)
		}
		add(index.where)
	}
	for _, exclusion := range table.exclusions {
		for _, pair := range exclusion.exclusions {
			add(pair.expression)
		}
		add(exclusion.where)
	}
	for _, policy := range table.policies {
		add(policy.using)
		add(policy.withCheck)
	}
}

// resolveDomainExpressionTypes resolves the types of the constraints of a domain, where VALUE has
// the base type of the domain
func (g *Generator) resolveDomainExpressionTypes(tc *typeContext, domain *Domain) {
	base, ok := tc.domainBaseType(domain)
	scope := func(col *parser.ColName) (exprType, bool) {
		if ok && col.Qualifier.IsEmpty() && !col.Name.Quoted && parser.FoldASCII(col.Name.Name) == "value" {
			return exprType{name: base}, true
		}
		return exprType{}, false
	}
	for _, constraint := range domain.constraints {
		if hashableExpr(constraint.expression) {
			g.typedExprs[constraint.expression] = resolveForms(tc, func() parser.Expr {
				resolved, _ := tc.resolve(constraint.expression, scope)
				return resolved
			})
		}
	}
}

// hashableExpr reports whether an expression can be a map key, which takes a pointer
func hashableExpr(expr parser.Expr) bool {
	return reflect.TypeOf(expr).Kind() == reflect.Pointer
}

// typed returns two expressions of tables with the casts that resolving their types implies,
// for comparing them
func (g *Generator) typed(a, b parser.Expr) (parser.Expr, parser.Expr) {
	return typedPair(g.exprForms(a), g.exprForms(b))
}

func (g *Generator) exprForms(expr parser.Expr) typedForms[parser.Expr] {
	if expr != nil && hashableExpr(expr) {
		if forms, ok := g.typedExprs[expr]; ok {
			return forms
		}
	}
	return typedForms[parser.Expr]{plain: expr, marked: expr}
}

// typedViews returns the definitions of two views with the casts that resolving their types
// implies, for comparing them
func (g *Generator) typedViews(a, b parser.SelectStatement) (parser.SelectStatement, parser.SelectStatement) {
	return typedPair(g.viewForms(a), g.viewForms(b))
}

func (g *Generator) viewForms(definition parser.SelectStatement) typedForms[parser.SelectStatement] {
	if forms, ok := g.typedViewDefinitions[definition]; ok {
		return forms
	}
	return typedForms[parser.SelectStatement]{plain: definition, marked: definition}
}

// relationColumn is an output column of a relation
type relationColumn struct {
	name     Ident
	typ      exprType
	collated bool
}

// relation is a table, a view or another relation in the FROM clause of a query, which a column
// reference may name by its alias. known is false for a relation whose columns sqldef does not
// know, such as a subquery or a function.
type relation struct {
	name    QualifiedName
	hasAs   bool
	known   bool
	columns []relationColumn
}

func (tc *typeContext) setRelations(tables []*Table, views []*View) {
	tc.tables, tc.views = tables, views
	tc.viewOutputs = map[*View]*viewOutput{}
}

// viewOutput holds the resolved columns of a view, once known
type viewOutput struct {
	known   bool
	columns []relationColumn
}

// viewColumns resolves the definition of a view, and returns its output columns
func (tc *typeContext) viewColumns(view *View) ([]relationColumn, bool) {
	if out, ok := tc.viewOutputs[view]; ok {
		return out.columns, out.known
	}
	// A view that reads itself while it is resolved finds its columns unknown
	out := &viewOutput{}
	tc.viewOutputs[view] = out
	outerCollated, outerCTEs := tc.collated, tc.ctes
	tc.collated, tc.ctes = false, nil
	forms := resolveForms(tc, func() parser.SelectStatement {
		return tc.resolveSelect(view.definition, nil, nil)
	})
	tc.g.typedViewDefinitions[view.definition] = forms
	out.columns, out.known = tc.outputColumns(view.definition, forms.marked, false)
	// PostgreSQL cannot replace a view with columns of other collations, which only dropping it
	// is sure to avoid
	if tc.collated {
		for i := range out.columns {
			out.columns[i].collated = true
		}
	} else if out.known && allColumnTypesKnown(out.columns) {
		tc.g.viewColumnTypes[view.definition] = out.columns
	}
	tc.collated, tc.ctes = outerCollated, outerCTEs
	return out.columns, out.known
}

func allColumnTypesKnown(columns []relationColumn) bool {
	for _, column := range columns {
		if column.name.IsEmpty() || column.typ.name == "" {
			return false
		}
	}
	return true
}

// outputColumns returns the names and types of the output columns of a query, from the query as
// written and as resolved. A query under a set operation leaves the type of its columns unknown,
// as the set operation resolves it with the other queries.
func (tc *typeContext) outputColumns(stmt, typed parser.SelectStatement, underSetOperation bool) ([]relationColumn, bool) {
	switch s := stmt.(type) {
	case *parser.ParenSelect:
		return tc.outputColumns(s.Select, typed.(*parser.ParenSelect).Select, underSetOperation)
	case *parser.Union:
		t := typed.(*parser.Union)
		left, ok := tc.outputColumns(s.Left, t.Left, true)
		if !ok {
			return nil, false
		}
		right, ok := tc.outputColumns(s.Right, t.Right, true)
		if !ok || len(left) != len(right) {
			return nil, false
		}
		columns := make([]relationColumn, len(left))
		for i := range left {
			columns[i].name = left[i].name
			if left[i].typ.name != "" && right[i].typ.name != "" {
				if common, ok := tc.resolver.CommonType([]string{left[i].typ.name, right[i].typ.name}); ok {
					columns[i].typ = commonType([]exprType{left[i].typ, right[i].typ}, common)
				}
			}
		}
		return columns, true
	case *parser.Select:
		t := typed.(*parser.Select)
		columns := make([]relationColumn, 0, len(s.SelectExprs))
		for i, selectExpr := range s.SelectExprs {
			aliased, ok := selectExpr.(*parser.AliasedExpr)
			if !ok {
				return nil, false
			}
			name := aliased.As
			if name.IsEmpty() {
				name, _ = selectExprOutputName(aliased.Expr, GeneratorModePostgres)
			}
			var typ exprType
			if expr := t.SelectExprs[i].(*parser.AliasedExpr).Expr; hashableExpr(expr) {
				typ = tc.g.exprTypes[expr]
			}
			// PostgreSQL makes an output column of unknown type text
			if typ.name == pgcatalog.Unknown && !underSetOperation {
				typ = exprType{name: "text"}
			}
			columns = append(columns, relationColumn{name: name, typ: typ})
		}
		return columns, true
	}
	return nil, false
}

// resolveSelect resolves the types of the expressions of a query. ctes are the name keys of
// the common table expressions in scope, which hide the tables and views of their names.
func (tc *typeContext) resolveSelect(stmt parser.SelectStatement, outer columnScope, ctes map[string]cteColumns) parser.SelectStatement {
	switch s := stmt.(type) {
	case *parser.Union:
		var with *parser.With
		ctes, with = tc.withCTEs(ctes, s.With)
		resolved := *s
		resolved.With = with
		resolved.Left = tc.resolveSelect(s.Left, outer, ctes)
		resolved.Right = tc.resolveSelect(s.Right, outer, ctes)
		return &resolved
	case *parser.ParenSelect:
		return &parser.ParenSelect{Select: tc.resolveSelect(s.Select, outer, ctes)}
	case *parser.Select:
		var with *parser.With
		ctes, with = tc.withCTEs(ctes, s.With)
		outerCTEs := tc.ctes
		tc.ctes = ctes
		defer func() { tc.ctes = outerCTEs }()
		var relations []relation
		for _, tableExpr := range s.From {
			relations = tc.fromRelations(tableExpr, ctes, relations)
		}
		scope := tc.relationScope(relations, outer)
		resolved := *s
		resolved.With = with
		resolved.SelectExprs = make(parser.SelectExprs, len(s.SelectExprs))
		for i, selectExpr := range s.SelectExprs {
			if aliased, ok := selectExpr.(*parser.AliasedExpr); ok {
				expr, t := tc.resolve(aliased.Expr, scope)
				expr = tc.markResolved(expr)
				// The cast names a column without an alias, even where PostgreSQL drops it
				if castType := writtenCastType(aliased.Expr); castType != nil && aliased.As.IsEmpty() {
					if _, isCast := expr.(*parser.CastExpr); !isCast {
						expr = &parser.CastExpr{Expr: expr, Type: castType}
					}
				}
				if t.name != "" && hashableExpr(expr) {
					tc.g.exprTypes[expr] = t
				}
				resolved.SelectExprs[i] = &parser.AliasedExpr{Expr: expr, As: aliased.As}
			} else {
				resolved.SelectExprs[i] = selectExpr
			}
		}
		resolved.From = make(parser.TableExprs, len(s.From))
		for i, tableExpr := range s.From {
			resolved.From[i] = tc.resolveJoinConditions(tableExpr, scope)
		}
		if s.Where != nil {
			expr, _ := tc.resolve(s.Where.Expr, scope)
			resolved.Where = &parser.Where{Type: s.Where.Type, Expr: expr}
		}
		if s.Having != nil {
			expr, _ := tc.resolve(s.Having.Expr, scope)
			resolved.Having = &parser.Where{Type: s.Having.Type, Expr: expr}
		}
		return &resolved
	}
	return stmt
}

// domainBaseType returns the base type of a domain
func (tc *typeContext) domainBaseType(domain *Domain) (string, bool) {
	name := domain.baseType
	if domain.baseTimezone {
		name += " with time zone"
	}
	if domain.baseArray {
		name += "[]"
	}
	return tc.typeOfName(name)
}

// writtenCastType returns the type of an expression written as x::t or CAST(x AS t)
func writtenCastType(expr parser.Expr) *parser.ConvertType {
	switch e := expr.(type) {
	case *parser.CastExpr:
		return e.Type
	case *parser.ConvertExpr:
		if e.Action == parser.CastStr && e.Style == nil {
			return e.Type
		}
	}
	return nil
}

// cteColumns are the output columns of a common table expression, if known
type cteColumns struct {
	columns []relationColumn
	known   bool
}

// withCTEs adds the common table expressions of a WITH clause to those in scope. Each one sees
// those before it, and where the WITH clause is recursive, itself and those after it with unknown
// columns.
func (tc *typeContext) withCTEs(ctes map[string]cteColumns, with *parser.With) (map[string]cteColumns, *parser.With) {
	if with == nil {
		return ctes, nil
	}
	resolvedWith := &parser.With{Recursive: with.Recursive}
	result := maps.Clone(ctes)
	if result == nil {
		result = map[string]cteColumns{}
	}
	if with.Recursive {
		for _, cte := range with.CTEs {
			result[tc.identKey(cte.Name)] = cteColumns{}
		}
	}
	for _, cte := range with.CTEs {
		key := tc.identKey(cte.Name)
		definition := tc.resolveSelect(cte.Definition, nil, result)
		columns, known := tc.outputColumns(cte.Definition, definition, false)
		// The column names of a CTE rename its leading columns
		if known && len(cte.Columns) > len(columns) {
			known = false
		} else if known {
			for i, name := range cte.Columns {
				columns[i].name = name
			}
		}
		result[key] = cteColumns{columns: columns, known: known}
		resolvedWith.CTEs = append(resolvedWith.CTEs, &parser.CommonTableExpr{Name: cte.Name, Columns: cte.Columns, Definition: definition})
	}
	return result, resolvedWith
}

func (tc *typeContext) identKey(ident parser.Ident) string {
	return normalizeIdentKey(ident, tc.g.mode, tc.g.legacyIgnoreQuotes, tc.g.config.MysqlLowerCaseTableNames)
}

// fromRelations adds the relations of a FROM item
func (tc *typeContext) fromRelations(tableExpr parser.TableExpr, ctes map[string]cteColumns, relations []relation) []relation {
	switch t := tableExpr.(type) {
	case *parser.JoinTableExpr:
		relations = tc.fromRelations(t.LeftExpr, ctes, relations)
		return tc.fromRelations(t.RightExpr, ctes, relations)
	case *parser.ParenTableExpr:
		for _, expr := range t.Exprs {
			relations = tc.fromRelations(expr, ctes, relations)
		}
		return relations
	case *parser.AliasedTableExpr:
		rel := relation{name: QualifiedName{Name: t.As}, hasAs: !t.As.IsEmpty()}
		if name, ok := t.Expr.(parser.TableName); ok {
			if !rel.hasAs {
				rel.name = QualifiedName{Schema: name.Schema, Name: name.Name}
			}
			if cte, isCTE := ctes[tc.identKey(name.Name)]; name.Schema.IsEmpty() && isCTE {
				if len(t.Columns) == 0 {
					rel.columns, rel.known = cte.columns, cte.known
				}
			} else if len(t.Columns) == 0 {
				rel.columns, rel.known = tc.relationColumns(QualifiedName{Schema: name.Schema, Name: name.Name})
			}
		}
		return append(relations, rel)
	}
	return append(relations, relation{})
}

// relationColumns returns the columns of a table or a view
func (tc *typeContext) relationColumns(name QualifiedName) ([]relationColumn, bool) {
	if table := tc.g.findTableByName(tc.tables, name); table != nil {
		columns := make([]relationColumn, 0, len(table.columns))
		for _, column := range getSortedColumns(table.columns) {
			t, _ := tc.columnType(column)
			columns = append(columns, relationColumn{name: column.name, typ: t, collated: column.collate != "" || tc.collatedDomains[t.name]})
		}
		return columns, true
	}
	for _, view := range tc.views {
		if tc.g.qualifiedNamesEqual(view.name, name) {
			return tc.viewColumns(view)
		}
	}
	return nil, false
}

// relationScope resolves the columns of the relations of a query, and the others in the
// enclosing queries
func (tc *typeContext) relationScope(relations []relation, outer columnScope) columnScope {
	return func(col *parser.ColName) (exprType, bool) {
		if !col.Qualifier.Name.IsEmpty() {
			for _, rel := range relations {
				if !tc.relationNamed(rel, col.Qualifier) {
					continue
				}
				if !rel.known {
					return exprType{}, false
				}
				if column, ok := tc.relationColumn(rel, col.Name); ok && column.typ.name != "" {
					tc.collated = tc.collated || column.collated
					return column.typ, true
				}
				return exprType{}, false
			}
		} else {
			var found []relationColumn
			for _, rel := range relations {
				if !rel.known {
					return exprType{}, false
				}
				if column, ok := tc.relationColumn(rel, col.Name); ok {
					found = append(found, column)
				}
			}
			switch len(found) {
			case 0:
			case 1:
				tc.collated = tc.collated || found[0].collated
				return found[0].typ, found[0].typ.name != ""
			default:
				return exprType{}, false
			}
		}
		if outer == nil {
			return exprType{}, false
		}
		return outer(col)
	}
}

func (tc *typeContext) relationColumn(rel relation, name Ident) (relationColumn, bool) {
	for _, column := range rel.columns {
		if !column.name.IsEmpty() && tc.g.identsEqual(column.name, name) {
			return column, true
		}
	}
	return relationColumn{}, false
}

func (tc *typeContext) relationNamed(rel relation, qualifier parser.TableName) bool {
	if rel.name.Name.IsEmpty() || !tc.g.identsEqual(rel.name.Name, qualifier.Name) {
		return false
	}
	if qualifier.Schema.IsEmpty() {
		return true
	}
	return !rel.hasAs && tc.g.qualifiedNamesEqual(rel.name, QualifiedName{Schema: qualifier.Schema, Name: qualifier.Name})
}

// resolveJoinConditions resolves the types of the ON conditions of the joins of a FROM item
func (tc *typeContext) resolveJoinConditions(tableExpr parser.TableExpr, scope columnScope) parser.TableExpr {
	switch t := tableExpr.(type) {
	case *parser.JoinTableExpr:
		resolved := *t
		resolved.LeftExpr = tc.resolveJoinConditions(t.LeftExpr, scope)
		resolved.RightExpr = tc.resolveJoinConditions(t.RightExpr, scope)
		if t.Condition.On != nil {
			resolved.Condition.On, _ = tc.resolve(t.Condition.On, scope)
		}
		return &resolved
	case *parser.ParenTableExpr:
		resolved := &parser.ParenTableExpr{Exprs: make(parser.TableExprs, len(t.Exprs))}
		for i, expr := range t.Exprs {
			resolved.Exprs[i] = tc.resolveJoinConditions(expr, scope)
		}
		return resolved
	}
	return tableExpr
}

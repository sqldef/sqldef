package postgres

import (
	"testing"

	"github.com/sqldef/sqldef/v3/database"
	"github.com/sqldef/sqldef/v3/parser"
	"github.com/sqldef/sqldef/v3/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A type modifier may hold identifiers rather than numbers. PostGIS spells one
// as geometry(Point,4326), and format_type() returns it as part of the type
// name, so the parser has to carry it through instead of rejecting the column.
//
// The identifiers come back downcased, as PostgreSQL downcases any unquoted
// identifier. That is harmless here: PostGIS canonicalizes the modifier itself
// (point and MULTIPOLYGON become Point and MultiPolygon in the catalog), and
// both sides of a diff are parsed the same way, so they still compare equal.
func TestParseParameterizedUserDefinedType(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{
			name: "identifier and srid",
			sql:  `CREATE TABLE t (g geometry(Point,4326))`,
			want: "geometry(point,4326)",
		},
		{
			name: "identifier only",
			sql:  `CREATE TABLE t (g geometry(Point))`,
			want: "geometry(point)",
		},
		{
			name: "geography",
			sql:  `CREATE TABLE t (g geography(Point,4326))`,
			want: "geography(point,4326)",
		},
		{
			// The two parsers spell a schema-qualified type differently: pgquery
			// keeps the schema in References while the generic parser puts it in
			// the type name. They converge in the schema layer, which prepends
			// References to the type name, so the modifier is what matters here.
			name: "schema-qualified",
			sql:  `CREATE TABLE t (g public.geometry(Point,4326))`,
			want: "geometry(point,4326) references `public.`",
		},
		{
			name: "no modifier is unchanged",
			sql:  `CREATE TABLE t (g geometry)`,
			want: "geometry",
		},
		{
			name: "numeric modifiers still use length and scale",
			sql:  `CREATE TABLE t (n numeric(10,2))`,
			want: "numeric(10,2)",
		},
		{
			name: "a single numeric modifier still uses length",
			sql:  `CREATE TABLE t (v vector(3))`,
			want: "vector(3)",
		},
	}

	// The mode is set on the struct rather than through NewParserWithMode, which
	// PSQLDEF_PARSER overrides -- CI runs the psqldef jobs with it set to
	// "generic", and this case has to exercise the pgquery path wherever it runs.
	p := PostgresParser{parser: database.NewParser(parser.ParserModePostgres), mode: PsqldefParserModePgquery}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmts, err := p.Parse(tt.sql)
			require.NoError(t, err)
			require.Len(t, stmts, 1)

			createTable, ok := stmts[0].Statement.(*parser.DDL)
			require.True(t, ok, "expected a DDL statement")
			require.NotNil(t, createTable.TableSpec)
			require.Len(t, createTable.TableSpec.Columns, 1)

			columnType := createTable.TableSpec.Columns[0].Type
			assert.Equal(t, tt.want, parser.String(&columnType))
		})
	}
}

// The exporter decides whether to quote a type name by looking for uppercase
// letters. A modifier holding an identifier would drag the whole string into
// quotes, producing a type that does not exist:
//
//	"geometry(Point,4326)"  ->  pq: type "geometry(Point,4326)" does not exist
func TestEscapeDataTypeNameKeepsModifierOutOfQuotes(t *testing.T) {
	d := &PostgresDatabase{}

	tests := []struct {
		typeName string
		want     string
	}{
		{"geometry", "geometry"},
		{"geometry(Point)", "geometry(Point)"},
		{"geometry(Point,4326)", "geometry(Point,4326)"},
		{"geography(Point,4326)", "geography(Point,4326)"},
		{"geometry(Point,4326)[]", "geometry(Point,4326)[]"},
		{"vector(3)", "vector(3)"},
		{"numeric(10,2)", "numeric(10,2)"},
		{"character varying(50)", "character varying(50)"},
		{"timestamp(0) with time zone", "timestamp(0) with time zone"},
		// A name that needs quoting still gets it, and only the name does.
		{"MyType", `"MyType"`},
		{"MyType(Point)", `"MyType"(Point)`},
		{"public.MyType", `public."MyType"`},
		// An already quoted name from format_type() is left alone.
		{`"MyType"`, `"MyType"`},
	}

	for _, tt := range tests {
		t.Run(tt.typeName, func(t *testing.T) {
			assert.Equal(t, tt.want, d.escapeDataTypeName(tt.typeName))
		})
	}
}

// The same columns must parse with the generic parser, which is what CI uses
// for the psqldef jobs (PSQLDEF_PARSER=generic) and what runs first in the
// default mode. Both parsers have to agree on the text they produce.
func TestParseParameterizedUserDefinedTypeWithGenericParser(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{"identifier and srid", `CREATE TABLE t (g geometry(Point,4326))`, "geometry(point,4326)"},
		{"identifier only", `CREATE TABLE t (g geometry(Point))`, "geometry(point)"},
		{"shape keyword that is not a builtin type name", `CREATE TABLE t (g geometry(MultiPolygon,4326))`, "geometry(multipolygon,4326)"},
		{"custom type that is not a grammar keyword", `CREATE TABLE t (g geography(Point,4326))`, "geography(point,4326)"},
		{"no modifier is unchanged", `CREATE TABLE t (g geometry)`, "geometry"},
		{"numeric modifiers still use length and scale", `CREATE TABLE t (n numeric(10,2))`, "numeric(10,2)"},
		// PostgreSQL qualifies the type when the extension's schema is not on the
		// search_path, which is what --export then writes.
		{"schema-qualified keyword type", `CREATE TABLE t (g public.geometry(Point,4326))`, "public.geometry(point,4326)"},
		{"schema-qualified keyword type without a modifier", `CREATE TABLE t (g public.geometry)`, "public.geometry"},
		{"schema-qualified custom type", `CREATE TABLE t (g public.geography(Point,4326))`, "public.geography(point,4326)"},
	}

	p := database.NewParser(parser.ParserModePostgres)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmts, err := p.Parse(tt.sql)
			require.NoError(t, err)
			require.Len(t, stmts, 1)

			createTable, ok := stmts[0].Statement.(*parser.DDL)
			require.True(t, ok, "expected a DDL statement")
			require.NotNil(t, createTable.TableSpec)
			require.Len(t, createTable.TableSpec.Columns, 1)

			columnType := createTable.TableSpec.Columns[0].Type
			assert.Equal(t, tt.want, parser.String(&columnType))
		})
	}
}

// Only PostgreSQL spells a type modifier this way, and the grammar is shared with the other
// dialects, so accepting one elsewhere would let a statement through that their server rejects.
// MySQL, for one, has no geometry(Point).
func TestParameterizedTypeIsRejectedOutsidePostgres(t *testing.T) {
	sqls := []string{
		`CREATE TABLE t (g geometry(Point,4326))`,
		`CREATE TABLE t (g mytype(10))`,
		`CREATE TABLE t (g mytype(a))`,
		`CREATE TABLE t (g s.geometry(Point,4326))`,
	}
	for name, mode := range map[string]parser.ParserMode{
		"mysql":   parser.ParserModeMysql,
		"mssql":   parser.ParserModeMssql,
		"sqlite3": parser.ParserModeSQLite3,
	} {
		p := database.NewParser(mode)
		for _, sql := range sqls {
			t.Run(name+"/"+sql, func(t *testing.T) {
				_, err := p.Parse(sql)
				assert.Error(t, err)
			})
		}
	}

	p := database.NewParser(parser.ParserModePostgres)
	for _, sql := range sqls {
		t.Run("postgres/"+sql, func(t *testing.T) {
			_, err := p.Parse(sql)
			assert.NoError(t, err)
		})
	}
}

// The two parsers have to represent the same column the same way. psqldef reads the schema it
// compares against with whichever parser accepts each side, so a modifier that one of them puts in
// Length and the other in TypeModifier would make a column that never changed come out altered.
func TestTypeModifierRepresentationMatchesPgquery(t *testing.T) {
	sql := `CREATE TABLE t (a halfvec(3), b varchar(10), c numeric(10,2), d vector(3), e geometry(Point,4326), f mytype(5))`

	generic := PostgresParser{parser: database.NewParser(parser.ParserModePostgres), mode: PsqldefParserModeGeneric}
	pgquery := PostgresParser{parser: database.NewParser(parser.ParserModePostgres), mode: PsqldefParserModePgquery}

	columnsOf := func(t *testing.T, p PostgresParser) []*parser.ColumnDefinition {
		t.Helper()
		stmts, err := p.Parse(sql)
		require.NoError(t, err)
		require.Len(t, stmts, 1)
		ddl, ok := stmts[0].Statement.(*parser.DDL)
		require.True(t, ok, "expected a DDL statement")
		require.NotNil(t, ddl.TableSpec)
		return ddl.TableSpec.Columns
	}

	genericColumns := columnsOf(t, generic)
	pgqueryColumns := columnsOf(t, pgquery)
	require.Len(t, genericColumns, len(pgqueryColumns))

	for i, column := range genericColumns {
		other := pgqueryColumns[i]
		t.Run(column.Name.Name, func(t *testing.T) {
			assert.Equal(t, other.Type.Length, column.Type.Length, "Length")
			assert.Equal(t, other.Type.Scale, column.Type.Scale, "Scale")
			assert.Equal(t, other.Type.TypeModifier, column.Type.TypeModifier, "TypeModifier")
			assert.Equal(t, other.Type.TypeIdent, column.Type.TypeIdent, "TypeIdent")
		})
	}
}

// PostgreSQL downcases an unquoted identifier and keeps a quoted one, and so does the modifier.
func TestTypeModifierKeepsQuotedIdentifierCase(t *testing.T) {
	p := database.NewParser(parser.ParserModePostgres)
	for _, tt := range []struct {
		sql  string
		want string
	}{
		{`CREATE TABLE t (g geometry("Point",4326))`, "geometry(Point,4326)"},
		{`CREATE TABLE t (g geometry(Point,4326))`, "geometry(point,4326)"},
	} {
		t.Run(tt.sql, func(t *testing.T) {
			stmts, err := p.Parse(tt.sql)
			require.NoError(t, err)
			require.Len(t, stmts, 1)
			ddl, ok := stmts[0].Statement.(*parser.DDL)
			require.True(t, ok, "expected a DDL statement")
			require.NotNil(t, ddl.TableSpec)
			require.Len(t, ddl.TableSpec.Columns, 1)
			columnType := ddl.TableSpec.Columns[0].Type
			assert.Equal(t, tt.want, parser.String(&columnType))
		})
	}
}

// The modifier has to reach the schema model, not just the CREATE TABLE that echoes the statement
// it was declared in: a column added later has to carry it, and a change to the modifier alone has
// to read as a change. This is the case the pull request exists for, so it is asserted end to end
// through the generator rather than on the parsed column.
func TestIdentifierTypeModifierReachesGenerator(t *testing.T) {
	tests := []struct {
		name    string
		current string
		desired string
		want    []string
	}{
		{
			name:    "add column keeps the modifier",
			current: `CREATE TABLE places (id int PRIMARY KEY);`,
			desired: `CREATE TABLE places (id int PRIMARY KEY, g geometry(Point,4326));`,
			want:    []string{`ALTER TABLE public.places ADD COLUMN g geometry(point,4326)`},
		},
		{
			name:    "a change to only the modifier is detected",
			current: `CREATE TABLE places (id int PRIMARY KEY, g geometry(Point,4326));`,
			desired: `CREATE TABLE places (id int PRIMARY KEY, g geometry(Polygon,4326));`,
			want:    []string{`ALTER TABLE public.places ALTER COLUMN g TYPE geometry(polygon,4326)`},
		},
		{
			name:    "a change to only the srid is detected",
			current: `CREATE TABLE places (id int PRIMARY KEY, g geometry(Point,4326));`,
			desired: `CREATE TABLE places (id int PRIMARY KEY, g geometry(Point,3857));`,
			want:    []string{`ALTER TABLE public.places ALTER COLUMN g TYPE geometry(point,3857)`},
		},
		{
			name:    "dropping the modifier is detected",
			current: `CREATE TABLE places (id int PRIMARY KEY, g geometry(Point,4326));`,
			desired: `CREATE TABLE places (id int PRIMARY KEY, g geometry);`,
			want:    []string{`ALTER TABLE public.places ALTER COLUMN g TYPE geometry`},
		},
		{
			name:    "a schema-qualified type behaves the same",
			current: `CREATE TABLE places (id int PRIMARY KEY);`,
			desired: `CREATE TABLE places (id int PRIMARY KEY, g public.geometry(Point,4326));`,
			want:    []string{`ALTER TABLE public.places ADD COLUMN g public.geometry(point,4326)`},
		},
		{
			name:    "an unchanged column is left alone",
			current: `CREATE TABLE places (id int PRIMARY KEY, g geometry(Point,4326));`,
			desired: `CREATE TABLE places (id int PRIMARY KEY, g geometry(Point,4326));`,
			want:    []string{},
		},
	}

	for name, mode := range map[string]PsqldefParserMode{
		"generic": PsqldefParserModeGeneric,
		"pgquery": PsqldefParserModePgquery,
	} {
		p := PostgresParser{parser: database.NewParser(parser.ParserModePostgres), mode: mode}
		for _, tt := range tests {
			t.Run(name+"/"+tt.name, func(t *testing.T) {
				ddls, err := schema.GenerateIdempotentDDLs(schema.GeneratorModePostgres, p, tt.desired, tt.current, database.GeneratorConfig{LegacyIgnoreQuotes: false}, "public")
				require.NoError(t, err)
				assert.Equal(t, tt.want, ddls)
			})
		}
	}
}

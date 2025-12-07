/*
Copyright 2017 Google Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package parser

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

type ParserMode int

const (
	eofChar = 0x100

	ParserModeMysql = ParserMode(iota)
	ParserModePostgres
	ParserModeSQLite3
	ParserModeMssql
)

// The main parser function for sqldef.
func ParseDDL(sql string, mode ParserMode) (Statement, error) {
	tokenizer := NewTokenizer(sql, mode)
	if yyParse(tokenizer) != 0 {
		return nil, fmt.Errorf(
			"found syntax error when parsing DDL \"%s\": %v", sql, tokenizer.LastError,
		)
	}
	return tokenizer.ParseTree, nil
}

// Tokenizer is the struct used to generate SQL
// tokens for the parser.
type Tokenizer struct {
	AllowComments        bool
	lastChar             rune
	Position             int
	lastToken            string
	LastError            error
	posVarIndex          int
	ParseTree            Statement
	partialDDL           *DDL
	multi                bool
	specialComment       *Tokenizer
	mode                 ParserMode
	peeking              bool // true when peeking ahead to avoid infinite recursion
	lastIdentifierQuoted bool // true if the last scanned identifier was quoted

	buf     string
	bufPos  int
	bufSize int
}

// NewTokenizer creates a new Tokenizer for a given SQL string.
func NewTokenizer(sql string, mode ParserMode) *Tokenizer {
	return &Tokenizer{
		buf:     sql,
		bufSize: len(sql),
		mode:    mode,
	}
}

// keywords maps keyword strings to their token IDs.
// Keywords marked as UNUSED are recognized but not actively used in the grammar.
//
// When adding new keywords, also add them to either the reserved_keyword or
// non_reserved_keyword grammar in parser.y. This allows the keyword to be used
// as an identifier in certain contexts.
var keywords = map[string]int{
	"accessible":             UNUSED,
	"action":                 ACTION,
	"add":                    ADD,
	"after":                  AFTER,
	"against":                AGAINST,
	"all":                    ALL,
	"alter":                  ALTER,
	"always":                 ALWAYS,
	"analyze":                ANALYZE,
	"and":                    AND,
	"array":                  ARRAY,
	"as":                     AS,
	"asc":                    ASC,
	"asensitive":             UNUSED,
	"auto_increment":         AUTO_INCREMENT,
	"autoincrement":          AUTOINCREMENT,
	"before":                 BEFORE,
	"begin":                  BEGIN,
	"between":                BETWEEN,
	"bigint":                 BIGINT,
	"bigserial":              BIGSERIAL,
	"binary":                 BINARY,
	"_binary":                UNDERSCORE_BINARY,
	"bit":                    BIT,
	"blob":                   BLOB,
	"bool":                   BOOL,
	"boolean":                BOOLEAN,
	"both":                   UNUSED,
	"bpchar":                 BPCHAR,
	"by":                     BY,
	"cache":                  CACHE,
	"call":                   UNUSED,
	"called":                 CALLED,
	"cascade":                CASCADE,
	"case":                   CASE,
	"cast":                   CAST,
	"change":                 UNUSED,
	"char":                   CHAR,
	"character":              CHARACTER,
	"charset":                CHARSET,
	"check":                  CHECK,
	"citext":                 CITEXT,
	"close":                  CLOSE,
	"clustered":              CLUSTERED,
	"nonclustered":           NONCLUSTERED,
	"collate":                COLLATE,
	"column":                 COLUMN,
	"columnstore":            COLUMNSTORE,
	"comment":                COMMENT_KEYWORD,
	"committed":              COMMITTED,
	"commit":                 COMMIT,
	"concurrently":           CONCURRENTLY,
	"async":                  ASYNC,
	"condition":              UNUSED,
	"constraint":             CONSTRAINT,
	"continue":               CONTINUE,
	"create":                 CREATE,
	"convert":                CONVERT,
	"cosine":                 COSINE,
	"cost":                   COST,
	"substr":                 SUBSTR,
	"substring":              SUBSTRING,
	"cross":                  CROSS,
	"current_date":           CURRENT_DATE,
	"current_time":           CURRENT_TIME,
	"current_timestamp":      CURRENT_TIMESTAMP,
	"current_user":           CURRENT_USER,
	"cursor":                 CURSOR,
	"cycle":                  CYCLE,
	"database":               DATABASE,
	"databases":              DATABASES,
	"day_hour":               UNUSED,
	"day_microsecond":        UNUSED,
	"day_minute":             UNUSED,
	"day_second":             UNUSED,
	"date":                   DATE,
	"data":                   DATA,
	"daterange":              DATERANGE,
	"datetime":               DATETIME,
	"datetime2":              DATETIME2,
	"datetimeoffset":         DATETIMEOFFSET,
	"deallocate":             DEALLOCATE,
	"dec":                    UNUSED,
	"decimal":                DECIMAL,
	"declare":                DECLARE,
	"default":                DEFAULT,
	"deferrable":             DEFERRABLE,
	"deferred":               DEFERRED,
	"definer":                DEFINER,
	"delayed":                UNUSED,
	"delete":                 DELETE,
	"desc":                   DESC,
	"describe":               DESCRIBE,
	"deterministic":          UNUSED,
	"distance":               DISTANCE,
	"domain":                 DOMAIN,
	"distinct":               DISTINCT,
	"distinctrow":            UNUSED,
	"div":                    DIV,
	"double":                 DOUBLE,
	"drop":                   DROP,
	"duplicate":              DUPLICATE,
	"each":                   EACH,
	"else":                   ELSE,
	"elseif":                 UNUSED,
	"enclosed":               UNUSED,
	"end":                    END,
	"enum":                   ENUM,
	"euclidean":              EUCLIDEAN,
	"escape":                 ESCAPE,
	"escaped":                UNUSED,
	"exclude":                EXCLUDE,
	"exists":                 EXISTS,
	"exec":                   EXEC,
	"execute":                EXECUTE,
	"except":                 EXCEPT,
	"exit":                   EXIT,
	"explain":                EXPLAIN,
	"extension":              EXTENSION,
	"expansion":              EXPANSION,
	"extract":                EXTRACT,
	"extended":               EXTENDED,
	"false":                  FALSE,
	"fetch":                  FETCH,
	"first":                  FIRST,
	"float":                  FLOAT_TYPE,
	"float4":                 UNUSED,
	"float8":                 UNUSED,
	"for":                    FOR,
	"force":                  FORCE,
	"foreign":                FOREIGN,
	"found":                  FOUND,
	"from":                   FROM,
	"full":                   FULL,
	"fulltext":               FULLTEXT,
	"function":               FUNCTION,
	"generated":              GENERATED,
	"geometry":               GEOMETRY,
	"geometrycollection":     GEOMETRYCOLLECTION,
	"get":                    UNUSED,
	"getdate":                GETDATE,
	"global":                 GLOBAL,
	"grant":                  GRANT,
	"group":                  GROUP,
	"group_concat":           GROUP_CONCAT,
	"handler":                HANDLER,
	"having":                 HAVING,
	"high_priority":          UNUSED,
	"holdlock":               HOLDLOCK,
	"hour_microsecond":       UNUSED,
	"hour_minute":            UNUSED,
	"hour_second":            UNUSED,
	"identity":               IDENTITY,
	"if":                     IF,
	"ignore":                 IGNORE,
	"immediate":              IMMEDIATE,
	"immutable":              IMMUTABLE,
	"in":                     IN,
	"include":                INCLUDE,
	"increment":              INCREMENT,
	"index":                  INDEX,
	"infile":                 UNUSED,
	"input":                  INPUT,
	"inout":                  UNUSED,
	"inner":                  INNER,
	"initially":              INITIALLY,
	"insensitive":            UNUSED,
	"insert":                 INSERT,
	"instead":                INSTEAD,
	"int":                    INT,
	"int1":                   UNUSED,
	"int2":                   UNUSED,
	"int3":                   UNUSED,
	"int4":                   UNUSED,
	"int4range":              INT4RANGE,
	"int8":                   UNUSED,
	"int8range":              INT8RANGE,
	"integer":                INTEGER,
	"intersect":              INTERSECT,
	"interval":               INTERVAL,
	"into":                   INTO,
	"invoker":                INVOKER,
	"io_after_gtids":         UNUSED,
	"is":                     IS,
	"isolation":              ISOLATION,
	"inherit":                INHERIT,
	"iterate":                UNUSED,
	"join":                   JOIN,
	"json":                   JSON,
	"jsonb":                  JSONB,
	"key":                    KEY,
	"keys":                   KEYS,
	"key_block_size":         KEY_BLOCK_SIZE,
	"kill":                   UNUSED,
	"language":               LANGUAGE,
	"leakproof":              LEAKPROOF,
	"last":                   LAST,
	"last_insert_id":         LAST_INSERT_ID,
	"leading":                UNUSED,
	"leave":                  UNUSED,
	"left":                   LEFT,
	"less":                   LESS,
	"level":                  LEVEL,
	"like":                   LIKE,
	"limit":                  LIMIT,
	"linear":                 UNUSED,
	"lines":                  UNUSED,
	"linestring":             LINESTRING,
	"load":                   UNUSED,
	"localtime":              LOCALTIME,
	"localtimestamp":         LOCALTIMESTAMP,
	"lock":                   LOCK,
	"long":                   UNUSED,
	"longblob":               LONGBLOB,
	"longtext":               LONGTEXT,
	"loop":                   UNUSED,
	"low_priority":           UNUSED,
	"m":                      M,
	"master_bind":            UNUSED,
	"match":                  MATCH,
	"materialized":           MATERIALIZED,
	"maxvalue":               MAXVALUE,
	"mediumblob":             MEDIUMBLOB,
	"mediumint":              MEDIUMINT,
	"mediumtext":             MEDIUMTEXT,
	"middleint":              UNUSED,
	"minute_microsecond":     UNUSED,
	"minute_second":          UNUSED,
	"minvalue":               MINVALUE,
	"mod":                    MOD,
	"mode":                   MODE,
	"modifies":               UNUSED,
	"money":                  MONEY,
	"multilinestring":        MULTILINESTRING,
	"multipoint":             MULTIPOINT,
	"multipolygon":           MULTIPOLYGON,
	"names":                  NAMES,
	"natural":                NATURAL,
	"nchar":                  NCHAR,
	"new":                    NEW,
	"next":                   NEXT,
	"no":                     NO,
	"nolock":                 NOLOCK,
	"none":                   NONE,
	"not":                    NOT,
	"now":                    NOW,
	"nowait":                 NOWAIT,
	"no_write_to_binlog":     UNUSED,
	"ntext":                  NTEXT,
	"null":                   NULL,
	"numeric":                NUMERIC,
	"numrange":               NUMRANGE,
	"nvarchar":               NVARCHAR,
	"of":                     OF,
	"offset":                 OFFSET,
	"on":                     ON,
	"only":                   ONLY,
	"open":                   OPEN,
	"optimize":               OPTIMIZE,
	"optimizer_costs":        UNUSED,
	"option":                 OPTION,
	"optionally":             UNUSED,
	"or":                     OR,
	"order":                  ORDER,
	"out":                    UNUSED,
	"outer":                  OUTER,
	"outfile":                UNUSED,
	"output":                 OUTPUT,
	"over":                   OVER,
	"owned":                  OWNED,
	"paglock":                PAGLOCK,
	"parallel":               PARALLEL,
	"parser":                 PARSER,
	"partition":              PARTITION,
	"permissive":             PERMISSIVE,
	"point":                  POINT,
	"policy":                 POLICY,
	"polygon":                POLYGON,
	"precision":              PRECISION,
	"primary":                PRIMARY,
	"prior":                  PRIOR,
	"privileges":             PRIVILEGES,
	"processlist":            PROCESSLIST,
	"procedure":              PROCEDURE,
	"query":                  QUERY,
	"restrictive":            RESTRICTIVE,
	"range":                  UNUSED,
	"read":                   READ,
	"reads":                  UNUSED,
	"read_write":             UNUSED,
	"readuncommitted":        READUNCOMMITTED,
	"real":                   REAL,
	"recursive":              RECURSIVE,
	"references":             REFERENCES,
	"regexp":                 REGEXP,
	"release":                UNUSED,
	"rename":                 RENAME,
	"reorganize":             REORGANIZE,
	"repair":                 REPAIR,
	"repeat":                 UNUSED,
	"repeatable":             REPEATABLE,
	"replace":                REPLACE,
	"replication":            REPLICATION,
	"require":                UNUSED,
	"resignal":               UNUSED,
	"restrict":               RESTRICT,
	"restricted":             RESTRICTED,
	"return":                 RETURN,
	"returns":                RETURNS,
	"revoke":                 REVOKE,
	"right":                  RIGHT,
	"rlike":                  REGEXP,
	"rollback":               ROLLBACK,
	"row":                    ROW,
	"rowid":                  ROWID,
	"rowlock":                ROWLOCK,
	"rows":                   ROWS,
	"safe":                   SAFE,
	"schema":                 SCHEMA,
	"schemas":                UNUSED,
	"scroll":                 SCROLL,
	"second_microsecond":     UNUSED,
	"security":               SECURITY,
	"select":                 SELECT,
	"sensitive":              UNUSED,
	"separator":              SEPARATOR,
	"sequence":               UNUSED,
	"serial":                 SERIAL,
	"serializable":           SERIALIZABLE,
	"session":                SESSION,
	"set":                    SET,
	"setof":                  SETOF,
	"share":                  SHARE,
	"show":                   SHOW,
	"signal":                 UNUSED,
	"signed":                 SIGNED,
	"smalldatetime":          SMALLDATETIME,
	"smallint":               SMALLINT,
	"smallmoney":             SMALLMONEY,
	"smallserial":            SMALLSERIAL,
	"spatial":                SPATIAL,
	"specific":               UNUSED,
	"sql":                    SQL,
	"sqlexception":           SQLEXCEPTION,
	"sqlstate":               SQLSTATE,
	"sqlwarning":             SQLWARNING,
	"sql_big_result":         UNUSED,
	"sql_cache":              SQL_CACHE,
	"sql_calc_found_rows":    UNUSED,
	"sql_no_cache":           SQL_NO_CACHE,
	"sql_small_result":       UNUSED,
	"srid":                   SRID,
	"ssl":                    UNUSED,
	"stable":                 STABLE,
	"start":                  START,
	"starting":               UNUSED,
	"status":                 STATUS,
	"stored":                 STORED,
	"straight_join":          STRAIGHT_JOIN,
	"stream":                 STREAM,
	"strict":                 STRICT,
	"table":                  TABLE,
	"tables":                 TABLES,
	"tablock":                TABLOCK,
	"terminated":             UNUSED,
	"text":                   TEXT,
	"text_pattern_ops":       TEXT_PATTERN_OPS,
	"than":                   THAN,
	"then":                   THEN,
	"time":                   TIME,
	"timestamp":              TIMESTAMP,
	"tinyblob":               TINYBLOB,
	"tinyint":                TINYINT,
	"tinytext":               TINYTEXT,
	"to":                     TO,
	"trailing":               UNUSED,
	"transaction":            TRANSACTION,
	"trigger":                TRIGGER,
	"true":                   TRUE,
	"tsrange":                TSRANGE,
	"tstzrange":              TSTZRANGE,
	"truncate":               TRUNCATE,
	"type":                   TYPE,
	"uncommitted":            UNCOMMITTED,
	"undo":                   UNUSED,
	"union":                  UNION,
	"unique":                 UNIQUE,
	"unlock":                 UNUSED,
	"unsigned":               UNSIGNED,
	"unsafe":                 UNSAFE,
	"update":                 UPDATE,
	"updlock":                UPDLOCK,
	"usage":                  UNUSED,
	"use":                    USE,
	"using":                  USING,
	"utc_date":               UTC_DATE,
	"utc_time":               UTC_TIME,
	"utc_timestamp":          UTC_TIMESTAMP,
	"uuid":                   UUID,
	"value":                  VALUE,
	"values":                 VALUES,
	"variables":              VARIABLES,
	"variadic":               VARIADIC,
	"varbinary":              VARBINARY,
	"varchar":                VARCHAR,
	"varcharacter":           UNUSED,
	"varying":                VARYING,
	"vector":                 VECTOR,
	"virtual":                VIRTUAL,
	"view":                   VIEW,
	"volatile":               VOLATILE,
	"vschema_tables":         VSCHEMA_TABLES,
	"when":                   WHEN,
	"where":                  WHERE,
	"while":                  WHILE,
	"with":                   WITH,
	"without":                WITHOUT,
	"write":                  WRITE,
	"xor":                    UNUSED,
	"year":                   YEAR,
	"year_month":             UNUSED,
	"zerofill":               ZEROFILL,
	"zone":                   ZONE,
	"::":                     TYPECAST,
	"allow_page_locks":       ALLOW_PAGE_LOCKS,
	"allow_row_locks":        ALLOW_ROW_LOCKS,
	"fillfactor":             FILLFACTOR,
	"ignore_dup_key":         IGNORE_DUP_KEY,
	"off":                    OFF,
	"pad_index":              PAD_INDEX,
	"statistics_incremental": STATISTICS_INCREMENTAL,
	"statistics_norecompute": STATISTICS_NORECOMPUTE,
	"lead":                   LEAD,
	"lag":                    LAG,

	// SET options for SQL Server
	"concat_null_yields_null":  CONCAT_NULL_YIELDS_NULL,
	"cursor_close_on_commit":   CURSOR_CLOSE_ON_COMMIT,
	"quoted_identifier":        QUOTED_IDENTIFIER,
	"arithabort":               ARITHABORT,
	"fmtonly":                  FMTONLY,
	"nocount":                  NOCOUNT,
	"noexec":                   NOEXEC,
	"numeric_roundabort":       NUMERIC_ROUNDABORT,
	"ansi_defaults":            ANSI_DEFAULTS,
	"ansi_null_dflt_off":       ANSI_NULL_DFLT_OFF,
	"ansi_null_dflt_on":        ANSI_NULL_DFLT_ON,
	"ansi_nulls":               ANSI_NULLS,
	"ansi_padding":             ANSI_PADDING,
	"ansi_warnings":            ANSI_WARNINGS,
	"forceplan":                FORCEPLAN,
	"showplan_all":             SHOWPLAN_ALL,
	"showplan_text":            SHOWPLAN_TEXT,
	"showplan_xml":             SHOWPLAN_XML,
	"implicit_transactions":    IMPLICIT_TRANSACTIONS,
	"remote_proc_transactions": REMOTE_PROC_TRANSACTIONS,
	"xact_abort":               XACT_ABORT,
}

// keywordStrings contains the reverse mapping of token to keyword strings
var keywordStrings = map[int]string{}

// IsKeyword returns true if the given string is a SQL keyword.
// The check is case-insensitive.
func IsKeyword(s string) bool {
	_, ok := keywords[strings.ToLower(s)]
	return ok
}

var encodeRef = map[byte]byte{
	'\x00': '0',
	'\'':   '\'',
	'"':    '"',
	'\b':   'b',
	'\n':   'n',
	'\r':   'r',
	'\t':   't',
	26:     'Z', // ctl-Z
	'\\':   '\\',
}

// sqlEncodeMap specifies how to escape binary data with '\'.
// Complies to http://dev.mysql.com/doc/refman/5.1/en/string-syntax.html
var sqlEncodeMap [256]byte

// sqlDecodeMap is the reverse of sqlEncodeMap
var sqlDecodeMap [256]byte

// dontEscape tells you if a character should not be escaped.
var dontEscape = byte(255)

func init() {
	// Convert keywords to keywordStrings
	for str, id := range keywords {
		if id == UNUSED {
			continue
		}
		keywordStrings[id] = str
	}

	// Convert encodeRef to sqlEncodeMap and sqlDecodeMap
	for i := range sqlEncodeMap {
		sqlEncodeMap[i] = dontEscape
		sqlDecodeMap[i] = dontEscape
	}
	for i := range sqlEncodeMap {
		b := byte(i)
		if to, ok := encodeRef[b]; ok {
			sqlEncodeMap[b] = to
			sqlDecodeMap[to] = b
		}
	}
}

// Lex returns the next token form the Tokenizer.
// This function is used by go yacc.
func (tkn *Tokenizer) Lex(lval *yySymType) int {
	typ, val := tkn.Scan()
	for typ == COMMENT {
		if tkn.AllowComments {
			break
		}
		typ, val = tkn.Scan()
	}
	if typ == ID {
		// For ID tokens, create an Ident with value and quoted flag
		lval.ident = NewIdent(val, tkn.lastIdentifierQuoted)
	} else {
		// For other tokens, use the str field as before
		lval.str = val
	}
	tkn.lastToken = val
	return typ
}

func (tkn *Tokenizer) getLineInfo(position int) (lineNum int, lineContent string, columnNum int) {
	lineNum = 1
	lineStart := 0

	position = min(position, len(tkn.buf))

	for i := 0; i < position && i < len(tkn.buf); i++ {
		if tkn.buf[i] == '\n' {
			lineNum++
			lineStart = i + 1
		}
	}

	// Find the end of the current line
	lineEnd := lineStart
	for lineEnd < len(tkn.buf) && tkn.buf[lineEnd] != '\n' {
		lineEnd++
	}

	// Extract the line content
	if lineStart <= len(tkn.buf) && lineEnd <= len(tkn.buf) {
		lineContent = tkn.buf[lineStart:lineEnd]
	}

	// Calculate column number (position within the line)
	columnNum = position - lineStart + 1

	return lineNum, lineContent, columnNum
}

// Error is called by go yacc if there's a parsing error.
func (tkn *Tokenizer) Error(err string) {
	var buf strings.Builder

	lineNum, lineContent, columnNum := tkn.getLineInfo(tkn.Position)

	fmt.Fprintf(&buf, "%s at line %d, column %d", err, lineNum, columnNum)

	if tkn.lastToken != "" {
		fmt.Fprintf(&buf, " near '%s'", tkn.lastToken)
	}

	if lineContent != "" {
		fmt.Fprintf(&buf, "\n  %s", lineContent)

		// Add a pointer to show the exact position
		if columnNum > 0 {
			fmt.Fprintf(&buf, "\n  %s^", strings.Repeat(" ", columnNum-1))
		}
	}

	tkn.LastError = errors.New(buf.String())

	// Try and re-sync to the next statement
	if tkn.lastChar != ';' {
		tkn.skipStatement()
	}
}

// Scan scans the tokenizer for the next token and returns
// the token type and an optional value.
func (tkn *Tokenizer) Scan() (int, string) {
	if tkn.specialComment != nil {
		// Enter specialComment scan mode.
		// for scanning such kind of comment: /*! MySQL-specific code */
		specialComment := tkn.specialComment
		tok, val := specialComment.Scan()
		if tok != 0 {
			// return the specialComment scan result as the result
			return tok, val
		}
		// leave specialComment scan mode after all stream consumed.
		tkn.specialComment = nil
	}
	if tkn.lastChar == 0 {
		tkn.next()
	}

	tkn.skipBlank()
	switch ch := tkn.lastChar; {
	case isIdentifierFirstChar(ch):
		tkn.next()
		if ch == 'X' || ch == 'x' {
			if tkn.lastChar == '\'' {
				tkn.next()
				return tkn.scanHex()
			}
		}
		if ch == 'B' || ch == 'b' {
			if tkn.lastChar == '\'' {
				tkn.next()
				return tkn.scanBitLiteral()
			}
		}
		if ch == 'N' {
			if tkn.lastChar == '\'' {
				if tkn.mode == ParserModeMssql {
					tkn.next()
					return tkn.scanString('\'', UNICODE_STRING)
				}
			}
		}
		isDbSystemVariable := false
		if ch == '@' && tkn.lastChar == '@' {
			isDbSystemVariable = true
		}
		return tkn.scanIdentifier(ch, isDbSystemVariable)
	case isAsciiDigit(ch):
		return tkn.scanNumber(false)
	case ch == ':':
		return tkn.scanBindVar()
	case ch == ';' && tkn.multi:
		return 0, ""
	default:
		tkn.next()
		switch ch {
		case eofChar:
			return 0, ""
		case '=', ',', ';', '(', ')', '[', ']', '+', '*', '%', '^', '~':
			if tkn.mode == ParserModeMssql && ch == '[' {
				return tkn.scanLiteralIdentifier(']')
			}
			if tkn.mode == ParserModePostgres && ch == '~' {
				// Check for ~~ (LIKE) and ~~* (ILIKE) pattern operators
				if tkn.lastChar == '~' {
					tkn.next()
					if tkn.lastChar == '*' {
						tkn.next()
						return PATTERN_ILIKE, ""
					}
					return PATTERN_LIKE, ""
				}
				// Check for ~* (case-insensitive regex) or ~ (regex)
				if tkn.lastChar == '*' {
					tkn.next()
					return POSIX_REGEX_CI, ""
				}
				return POSIX_REGEX, ""
			}
			return int(ch), ""
		case '&':
			if tkn.lastChar == '&' {
				tkn.next()
				return AND, ""
			}
			return int(ch), ""
		case '|':
			if tkn.lastChar == '|' {
				tkn.next()
				return OR, ""
			}
			return int(ch), ""
		case '?':
			tkn.posVarIndex++
			return VALUE_ARG, fmt.Sprintf(":v%d", tkn.posVarIndex)
		case '.':
			if isAsciiDigit(tkn.lastChar) {
				return tkn.scanNumber(true)
			}
			return int(ch), ""
		case '/':
			switch tkn.lastChar {
			case '/':
				tkn.next()
				return tkn.scanCommentType1("//")
			case '*':
				tkn.next()
				switch tkn.lastChar {
				case '!':
					return tkn.scanMySQLSpecificComment()
				default:
					return tkn.scanCommentType2()
				}
			default:
				return int(ch), ""
			}
		case '#':
			return tkn.scanCommentType1("#")
		case '-':
			switch tkn.lastChar {
			case '-':
				tkn.next()
				return tkn.scanCommentType1("--")
			case '>':
				tkn.next()
				if tkn.lastChar == '>' {
					tkn.next()
					return JSON_UNQUOTE_EXTRACT_OP, ""
				}
				return JSON_EXTRACT_OP, ""
			}
			return int(ch), ""
		case '<':
			switch tkn.lastChar {
			case '>':
				tkn.next()
				return NE, ""
			case '<':
				tkn.next()
				return SHIFT_LEFT, ""
			case '=':
				tkn.next()
				switch tkn.lastChar {
				case '>':
					tkn.next()
					return NULL_SAFE_EQUAL, ""
				default:
					return LE, ""
				}
			default:
				return int(ch), ""
			}
		case '>':
			switch tkn.lastChar {
			case '=':
				tkn.next()
				return GE, ""
			case '>':
				tkn.next()
				return SHIFT_RIGHT, ""
			default:
				return int(ch), ""
			}
		case '!':
			if tkn.mode == ParserModePostgres {
				if tkn.lastChar == '~' {
					tkn.next()
					// Check for !~~* (NOT ILIKE) and !~~ (NOT LIKE)
					if tkn.lastChar == '~' {
						tkn.next()
						if tkn.lastChar == '*' {
							tkn.next()
							return PATTERN_NOT_ILIKE, ""
						}
						return PATTERN_NOT_LIKE, ""
					}
					// Check for !~* (NOT case-insensitive regex) or !~ (NOT regex)
					if tkn.lastChar == '*' {
						tkn.next()
						return POSIX_NOT_REGEX_CI, ""
					}
					return POSIX_NOT_REGEX, ""
				}
			}
			if tkn.lastChar == '=' {
				tkn.next()
				return NE, ""
			}
			return int(ch), ""
		case '\'':
			return tkn.scanString(ch, STRING)
		case '"':
			if tkn.mode != ParserModeMysql {
				return tkn.scanLiteralIdentifier('"')
			} else {
				return tkn.scanString(ch, STRING)
			}
		case '$':
			// PostgreSQL dollar-quoted strings: $$...$$ or $tag$...$tag$
			if tkn.mode == ParserModePostgres {
				return tkn.scanDollarQuotedString()
			}
			return LEX_ERROR, string(ch)
		default:
			if tkn.mode != ParserModePostgres && ch == '`' {
				return tkn.scanLiteralIdentifier('`')
			}
			return LEX_ERROR, string(ch)
		}
	}
}

// skipStatement scans until the EOF, or end of statement is encountered.
func (tkn *Tokenizer) skipStatement() {
	ch := tkn.lastChar
	for ch != ';' && ch != eofChar {
		tkn.next()
		ch = tkn.lastChar
	}
}

func (tkn *Tokenizer) skipBlank() {
	ch := tkn.lastChar
	for ch == ' ' || ch == '\n' || ch == '\r' || ch == '\t' {
		tkn.next()
		ch = tkn.lastChar
	}
}

func (tkn *Tokenizer) scanIdentifier(firstChar rune, isDbSystemVariable bool) (int, string) {
	var buffer strings.Builder
	buffer.WriteRune(firstChar)
	for isIdentifierFirstChar(tkn.lastChar) || isAsciiDigit(tkn.lastChar) || (isDbSystemVariable && isIdentifierMetaChar(tkn.lastChar)) {
		buffer.WriteRune(tkn.lastChar)
		tkn.next()
	}
	loweredStr := strings.ToLower(buffer.String())
	if keywordID, found := keywords[loweredStr]; found {
		// Context-aware handling for "with" keyword
		// Only peek if we're not already in a peek operation (prevents infinite recursion)
		if keywordID == WITH && !tkn.peeking {
			nextID, _ := tkn.peekToken()

			// If next token is DATA or NO (for "WITH NO DATA"), this is a data option
			if nextID == DATA || nextID == NO {
				return WITH_DATA_OPTION, loweredStr
			}
		}

		// keyword is case-insensitive
		return keywordID, loweredStr
	}

	// dual must always be case-insensitive
	if loweredStr == "dual" {
		tkn.lastIdentifierQuoted = false
		return ID, loweredStr
	}

	// others are case-sensitive (unquoted identifiers)
	tkn.lastIdentifierQuoted = false
	return ID, buffer.String()
}

func (tkn *Tokenizer) scanHex() (int, string) {
	var buffer strings.Builder
	tkn.scanMantissa(16, &buffer)
	if tkn.lastChar != '\'' {
		return LEX_ERROR, buffer.String()
	}
	tkn.next()
	if buffer.Len()%2 != 0 {
		return LEX_ERROR, buffer.String()
	}
	return HEX, buffer.String()
}

func (tkn *Tokenizer) scanBitLiteral() (int, string) {
	var buffer strings.Builder
	tkn.scanMantissa(2, &buffer)
	if tkn.lastChar != '\'' {
		return LEX_ERROR, buffer.String()
	}
	tkn.next()
	return BIT_LITERAL, buffer.String()
}

func (tkn *Tokenizer) scanLiteralIdentifier(sepChar rune) (int, string) {
	var buffer strings.Builder
	backTickSeen := false
	for {
		if backTickSeen {
			if tkn.lastChar != sepChar {
				break
			}
			backTickSeen = false
			buffer.WriteRune(sepChar)
			tkn.next()
			continue
		}
		// The previous char was not a backtick.
		switch tkn.lastChar {
		case sepChar:
			backTickSeen = true
		case eofChar:
			// Premature EOF.
			return LEX_ERROR, buffer.String()
		default:
			buffer.WriteRune(tkn.lastChar)
		}
		tkn.next()
	}
	if buffer.Len() == 0 {
		return LEX_ERROR, buffer.String()
	}
	// Literal identifiers are quoted
	tkn.lastIdentifierQuoted = true
	return ID, buffer.String()
}

func (tkn *Tokenizer) scanBindVar() (int, string) {
	var buffer strings.Builder
	buffer.WriteRune(tkn.lastChar)
	token := VALUE_ARG
	tkn.next()
	if tkn.lastChar == ':' {
		if tkn.mode == ParserModePostgres {
			buffer.WriteRune(tkn.lastChar)
			tkn.next()
			return TYPECAST, buffer.String()
		}
		token = LIST_ARG
		buffer.WriteRune(tkn.lastChar)
		tkn.next()
	}
	if !isIdentifierFirstChar(tkn.lastChar) {
		return LEX_ERROR, buffer.String()
	}
	for isIdentifierFirstChar(tkn.lastChar) || isAsciiDigit(tkn.lastChar) || tkn.lastChar == '.' {
		buffer.WriteRune(tkn.lastChar)
		tkn.next()
	}
	return token, buffer.String()
}

func (tkn *Tokenizer) scanMantissa(base int, buffer *strings.Builder) {
	for digitVal(tkn.lastChar) < base {
		tkn.consumeNext(buffer)
	}
}

func (tkn *Tokenizer) scanNumber(seenDecimalPoint bool) (int, string) {
	token := INTEGRAL
	var buffer strings.Builder
	if seenDecimalPoint {
		token = FLOAT
		buffer.WriteByte('.')
		tkn.scanMantissa(10, &buffer)
		goto exponent
	}

	// 0x construct.
	if tkn.lastChar == '0' {
		tkn.consumeNext(&buffer)
		if tkn.lastChar == 'x' || tkn.lastChar == 'X' {
			token = HEXNUM
			tkn.consumeNext(&buffer)
			tkn.scanMantissa(16, &buffer)
			goto exit
		}
	}

	tkn.scanMantissa(10, &buffer)

	if tkn.lastChar == '.' {
		token = FLOAT
		tkn.consumeNext(&buffer)
		tkn.scanMantissa(10, &buffer)
	}

exponent:
	if tkn.lastChar == 'e' || tkn.lastChar == 'E' {
		token = FLOAT
		tkn.consumeNext(&buffer)
		if tkn.lastChar == '+' || tkn.lastChar == '-' {
			tkn.consumeNext(&buffer)
		}
		tkn.scanMantissa(10, &buffer)
	}

exit:
	// A letter cannot immediately follow a number.
	if isIdentifierFirstChar(tkn.lastChar) {
		return LEX_ERROR, buffer.String()
	}

	return token, buffer.String()
}

func (tkn *Tokenizer) scanString(delim rune, typ int) (int, string) {
	var buffer strings.Builder
	for {
		ch := tkn.lastChar
		if ch == eofChar {
			// Unterminated string.
			return LEX_ERROR, buffer.String()
		}

		if ch != delim && ch != '\\' {
			buffer.WriteRune(ch)

			start := tkn.bufPos
			delimByte := byte(delim)
			for ; tkn.bufPos < tkn.bufSize; tkn.bufPos++ {
				b := tkn.buf[tkn.bufPos]
				if b == delimByte || b == '\\' {
					break
				}
			}

			buffer.WriteString(tkn.buf[start:tkn.bufPos])
			tkn.Position += (tkn.bufPos - start)

			if tkn.bufPos >= tkn.bufSize {
				// Reached the end of the buffer without finding a delim or
				// escape character.
				tkn.next()
				continue
			}

			tkn.next()
			continue
		}
		tkn.next()

		if ch == '\\' {
			if tkn.lastChar == eofChar {
				// String terminates mid escape character.
				return LEX_ERROR, buffer.String()
			}
			if decodedChar := sqlDecodeMap[byte(tkn.lastChar)]; decodedChar == dontEscape {
				ch = tkn.lastChar
			} else {
				ch = rune(decodedChar)
			}

		} else if ch == delim && tkn.lastChar != delim {
			// Correctly terminated string, which is not a double delim.
			break
		}

		buffer.WriteRune(ch)
		tkn.next()
	}

	return typ, buffer.String()
}

// scanDollarQuotedString scans a PostgreSQL dollar-quoted string.
// Supports both $$...$$ and $tag$...$tag$ styles.
func (tkn *Tokenizer) scanDollarQuotedString() (int, string) {
	var buffer strings.Builder

	// Build the opening delimiter (already consumed the first $)
	var delimiter strings.Builder
	delimiter.WriteByte('$')

	// Check if it's $$ or $tag$
	if tkn.lastChar == '$' {
		// Simple $$ delimiter
		delimiter.WriteByte('$')
		tkn.next()
	} else if isIdentifierFirstChar(tkn.lastChar) || isAsciiDigit(tkn.lastChar) {
		// Tagged $tag$ delimiter
		for isIdentifierFirstChar(tkn.lastChar) || isAsciiDigit(tkn.lastChar) {
			delimiter.WriteRune(tkn.lastChar)
			tkn.next()
		}
		if tkn.lastChar != '$' {
			return LEX_ERROR, delimiter.String()
		}
		delimiter.WriteByte('$')
		tkn.next()
	} else {
		return LEX_ERROR, "$"
	}

	delimStr := delimiter.String()

	// Scan until we find the closing delimiter
	for {
		if tkn.lastChar == eofChar {
			return LEX_ERROR, buffer.String()
		}

		if tkn.lastChar == '$' {
			// Check if this is the start of the closing delimiter
			savedPos := tkn.bufPos
			savedChar := tkn.lastChar
			savedPosition := tkn.Position

			var potentialDelim strings.Builder
			potentialDelim.WriteByte('$')
			tkn.next()

			for isIdentifierFirstChar(tkn.lastChar) || isAsciiDigit(tkn.lastChar) {
				potentialDelim.WriteRune(tkn.lastChar)
				tkn.next()
			}

			if tkn.lastChar == '$' {
				potentialDelim.WriteByte('$')
				tkn.next()

				if potentialDelim.String() == delimStr {
					// Found the closing delimiter
					return STRING, buffer.String()
				}
			}

			// Not the closing delimiter, restore and include in content
			buffer.WriteString(potentialDelim.String()[:potentialDelim.Len()-1])
			tkn.bufPos = savedPos
			tkn.lastChar = savedChar
			tkn.Position = savedPosition
			buffer.WriteRune(tkn.lastChar)
			tkn.next()
		} else {
			buffer.WriteRune(tkn.lastChar)
			tkn.next()
		}
	}
}

func (tkn *Tokenizer) scanCommentType1(prefix string) (int, string) {
	var buffer strings.Builder
	buffer.WriteString(prefix)
	for tkn.lastChar != eofChar {
		if tkn.lastChar == '\n' {
			tkn.consumeNext(&buffer)
			break
		}
		tkn.consumeNext(&buffer)
	}
	return COMMENT, buffer.String()
}

func (tkn *Tokenizer) scanCommentType2() (int, string) {
	var buffer strings.Builder
	buffer.WriteString("/*")
	for {
		if tkn.lastChar == '*' {
			tkn.consumeNext(&buffer)
			if tkn.lastChar == '/' {
				tkn.consumeNext(&buffer)
				break
			}
			continue
		}
		if tkn.lastChar == eofChar {
			return LEX_ERROR, buffer.String()
		}
		tkn.consumeNext(&buffer)
	}
	return COMMENT, buffer.String()
}

func (tkn *Tokenizer) scanMySQLSpecificComment() (int, string) {
	var buffer strings.Builder
	buffer.WriteString("/*!")
	tkn.next()
	for {
		if tkn.lastChar == '*' {
			tkn.consumeNext(&buffer)
			if tkn.lastChar == '/' {
				tkn.consumeNext(&buffer)
				break
			}
			continue
		}
		if tkn.lastChar == eofChar {
			return LEX_ERROR, buffer.String()
		}
		tkn.consumeNext(&buffer)
	}
	_, sql := extractMysqlComment(buffer.String())
	tkn.specialComment = NewTokenizer(sql, tkn.mode)
	return tkn.Scan()
}

func (tkn *Tokenizer) consumeNext(buffer *strings.Builder) {
	if tkn.lastChar == eofChar {
		// This should never happen.
		panic("unexpected EOF")
	}
	buffer.WriteRune(tkn.lastChar)
	tkn.next()
}

func (tkn *Tokenizer) next() {
	if tkn.bufPos >= tkn.bufSize {
		if tkn.lastChar != eofChar {
			tkn.Position++
			tkn.lastChar = eofChar
		}
	} else {
		r, size := utf8.DecodeRuneInString(tkn.buf[tkn.bufPos:])
		tkn.Position += size
		tkn.lastChar = r
		tkn.bufPos += size
	}
}

// peekToken peeks ahead to determine the next token
// without consuming any characters. This is used for context-aware tokenization.
func (tkn *Tokenizer) peekToken() (int, string) {
	// Save current state and restore on exit
	savedLastChar := tkn.lastChar
	savedPosition := tkn.Position
	savedBufPos := tkn.bufPos
	savedPeeking := tkn.peeking

	defer func() {
		tkn.peeking = savedPeeking
		tkn.lastChar = savedLastChar
		tkn.Position = savedPosition
		tkn.bufPos = savedBufPos
	}()

	// Set peeking flag to prevent infinite recursion
	tkn.peeking = true
	return tkn.Scan()
}

// extractMysqlComment extracts the version and SQL from a comment-only query
// such as /*!50708 sql here */
func extractMysqlComment(sql string) (version string, innerSQL string) {
	sql = sql[3 : len(sql)-2]

	digitCount := 0
	endOfVersionIndex := strings.IndexFunc(sql, func(c rune) bool {
		digitCount++
		return !isAsciiDigit(c) || digitCount == 6
	})
	version = sql[0:endOfVersionIndex]
	innerSQL = strings.TrimFunc(sql[endOfVersionIndex:], unicode.IsSpace)

	return version, innerSQL
}

func isIdentifierFirstChar(ch rune) bool {
	return 'a' <= ch && ch <= 'z' || 'A' <= ch && ch <= 'Z' || ch == '_' || ch == '@'
}

func isIdentifierMetaChar(ch rune) bool {
	return ch == '.' || ch == '\'' || ch == '"' || ch == '`'
}

func isAsciiDigit(ch rune) bool {
	return '0' <= ch && ch <= '9'
}

func digitVal(ch rune) int {
	switch {
	case '0' <= ch && ch <= '9':
		return int(ch) - '0'
	case 'a' <= ch && ch <= 'f':
		return int(ch) - 'a' + 10
	case 'A' <= ch && ch <= 'F':
		return int(ch) - 'A' + 10
	}
	return 16 // larger than any legal digit val
}

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
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/k0kubun/sqldef/parser/dependency/bytes2"
	"github.com/k0kubun/sqldef/parser/dependency/sqltypes"
)

type ParserMode int

const (
	defaultBufSize = 4096
	eofChar        = 0x100

	ParserModeMysql = ParserMode(iota)
	ParserModePostgres
	ParserModeSQLite3
	ParserModeMssql
)

// Tokenizer is the struct used to generate SQL
// tokens for the parser.
type Tokenizer struct {
	InStream       io.Reader
	AllowComments  bool
	ForceEOF       bool
	lastChar       uint16
	Position       int
	lastToken      []byte
	LastError      error
	posVarIndex    int
	ParseTree      Statement
	partialDDL     *DDL
	nesting        int
	multi          bool
	specialComment *Tokenizer
	mode           ParserMode

	buf     []byte
	bufPos  int
	bufSize int
}

// NewStringTokenizer creates a new Tokenizer for the
// sql string.
func NewStringTokenizer(sql string, mode ParserMode) *Tokenizer {
	buf := []byte(sql)
	return &Tokenizer{
		buf:     buf,
		bufSize: len(buf),
		mode:    mode,
	}
}

// NewTokenizer creates a new Tokenizer reading a sql
// string from the io.Reader.
func NewTokenizer(r io.Reader) *Tokenizer {
	return &Tokenizer{
		InStream: r,
		buf:      make([]byte, defaultBufSize),
	}
}

// keywords is a map of mysql keywords that fall into two categories:
// 1) keywords considered reserved by MySQL
// 2) keywords for us to handle specially in sql.y
//
// Those marked as UNUSED are likely reserved keywords. We add them here so that
// when rewriting queries we can properly backtick quote them so they don't cause issues
//
// NOTE: If you add new keywords, add them also to the reserved_keywords or
// non_reserved_keywords grammar in sql.y -- this will allow the keyword to be used
// in identifiers. See the docs for each grammar to determine which one to put it into.
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
	"condition":              UNUSED,
	"constraint":             CONSTRAINT,
	"continue":               UNUSED,
	"convert":                CONVERT,
	"substr":                 SUBSTR,
	"substring":              SUBSTRING,
	"create":                 CREATE,
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
	"escape":                 ESCAPE,
	"escaped":                UNUSED,
	"exists":                 EXISTS,
	"exit":                   UNUSED,
	"explain":                EXPLAIN,
	"expansion":              EXPANSION,
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
	"from":                   FROM,
	"full":                   FULL,
	"fulltext":               FULLTEXT,
	"generated":              GENERATED,
	"geometry":               GEOMETRY,
	"geometrycollection":     GEOMETRYCOLLECTION,
	"get":                    UNUSED,
	"getdate":                GETDATE,
	"global":                 GLOBAL,
	"grant":                  UNUSED,
	"group":                  GROUP,
	"group_concat":           GROUP_CONCAT,
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
	"in":                     IN,
	"include":                INCLUDE,
	"increment":              INCREMENT,
	"index":                  INDEX,
	"infile":                 UNUSED,
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
	"int8":                   UNUSED,
	"integer":                INTEGER,
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
	"nvarchar":               NVARCHAR,
	"of":                     OF,
	"offset":                 OFFSET,
	"on":                     ON,
	"only":                   ONLY,
	"open":                   OPEN,
	"optimize":               OPTIMIZE,
	"optimizer_costs":        UNUSED,
	"option":                 UNUSED,
	"optionally":             UNUSED,
	"or":                     OR,
	"order":                  ORDER,
	"out":                    UNUSED,
	"outer":                  OUTER,
	"outfile":                UNUSED,
	"over":                   OVER,
	"owned":                  OWNED,
	"paglock":                PAGLOCK,
	"parser":                 PARSER,
	"partition":              PARTITION,
	"permissive":             PERMISSIVE,
	"point":                  POINT,
	"policy":                 POLICY,
	"polygon":                POLYGON,
	"precision":              PRECISION,
	"primary":                PRIMARY,
	"prior":                  PRIOR,
	"processlist":            PROCESSLIST,
	"procedure":              PROCEDURE,
	"query":                  QUERY,
	"restrictive":            RESTRICTIVE,
	"range":                  UNUSED,
	"read":                   READ,
	"reads":                  UNUSED,
	"read_write":             UNUSED,
	"real":                   REAL,
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
	"return":                 UNUSED,
	"revoke":                 UNUSED,
	"right":                  RIGHT,
	"rlike":                  REGEXP,
	"rollback":               ROLLBACK,
	"row":                    ROW,
	"rowid":                  ROWID,
	"rowlock":                ROWLOCK,
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
	"sqlexception":           UNUSED,
	"sqlstate":               UNUSED,
	"sqlwarning":             UNUSED,
	"sql_big_result":         UNUSED,
	"sql_cache":              SQL_CACHE,
	"sql_calc_found_rows":    UNUSED,
	"sql_no_cache":           SQL_NO_CACHE,
	"sql_small_result":       UNUSED,
	"srid":                   SRID,
	"ssl":                    UNUSED,
	"start":                  START,
	"starting":               UNUSED,
	"status":                 STATUS,
	"stored":                 STORED,
	"straight_join":          STRAIGHT_JOIN,
	"stream":                 STREAM,
	"strict":                 STRICT,
	"table":                  TABLE,
	"tables":                 TABLES,
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
	"truncate":               TRUNCATE,
	"type":                   TYPE,
	"uncommitted":            UNCOMMITTED,
	"undo":                   UNUSED,
	"union":                  UNION,
	"unique":                 UNIQUE,
	"unlock":                 UNUSED,
	"unsigned":               UNSIGNED,
	"update":                 UPDATE,
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
	"virtual":                VIRTUAL,
	"vindex":                 VINDEX,
	"vindexes":               VINDEXES,
	"view":                   VIEW,
	"vitess_keyspaces":       VITESS_KEYSPACES,
	"vitess_shards":          VITESS_SHARDS,
	"vitess_tablets":         VITESS_TABLETS,
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
	// SET options
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

func init() {
	for str, id := range keywords {
		if id == UNUSED {
			continue
		}
		keywordStrings[id] = str
	}
}

// KeywordString returns the string corresponding to the given keyword
func KeywordString(id int) string {
	str, ok := keywordStrings[id]
	if !ok {
		return ""
	}
	return str
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
	lval.bytes = val
	tkn.lastToken = val
	return typ
}

// Error is called by go yacc if there's a parsing error.
func (tkn *Tokenizer) Error(err string) {
	buf := &bytes2.Buffer{}
	if tkn.lastToken != nil {
		fmt.Fprintf(buf, "%s at position %v near '%s'", err, tkn.Position, tkn.lastToken)
	} else {
		fmt.Fprintf(buf, "%s at position %v", err, tkn.Position)
	}
	tkn.LastError = errors.New(buf.String())

	// Try and re-sync to the next statement
	if tkn.lastChar != ';' {
		tkn.skipStatement()
	}
}

// Scan scans the tokenizer for the next token and returns
// the token type and an optional value.
func (tkn *Tokenizer) Scan() (int, []byte) {
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

	if tkn.ForceEOF {
		tkn.skipStatement()
		return 0, nil
	}

	tkn.skipBlank()
	switch ch := tkn.lastChar; {
	case isLetter(ch):
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
		return tkn.scanIdentifier(byte(ch), isDbSystemVariable)
	case isDigit(ch):
		return tkn.scanNumber(false)
	case ch == ':':
		return tkn.scanBindVar()
	case ch == ';' && tkn.multi:
		return 0, nil
	default:
		tkn.next()
		switch ch {
		case eofChar:
			return 0, nil
		case '=', ',', ';', '(', ')', '[', ']', '+', '*', '%', '^', '~':
			if tkn.mode == ParserModeMssql && ch == '[' {
				return tkn.scanLiteralIdentifier(']')
			}
			if tkn.mode == ParserModePostgres && ch == '~' {
				if tkn.lastChar == '*' {
					tkn.next()
					return POSIX_REGEX_CI, nil
				}
				return POSIX_REGEX, nil
			}
			return int(ch), nil
		case '&':
			if tkn.lastChar == '&' {
				tkn.next()
				return AND, nil
			}
			return int(ch), nil
		case '|':
			if tkn.lastChar == '|' {
				tkn.next()
				return OR, nil
			}
			return int(ch), nil
		case '?':
			tkn.posVarIndex++
			buf := new(bytes2.Buffer)
			fmt.Fprintf(buf, ":v%d", tkn.posVarIndex)
			return VALUE_ARG, buf.Bytes()
		case '.':
			if isDigit(tkn.lastChar) {
				return tkn.scanNumber(true)
			}
			return int(ch), nil
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
				return int(ch), nil
			}
		case '#':
			return tkn.scanCommentType1("#")
		case '-':
			if isDigit(tkn.lastChar) {
				num, buf := tkn.scanNumber(false)
				return num, append([]byte{'-'}, buf...)
			}
			switch tkn.lastChar {
			case '-':
				tkn.next()
				return tkn.scanCommentType1("--")
			case '>':
				tkn.next()
				if tkn.lastChar == '>' {
					tkn.next()
					return JSON_UNQUOTE_EXTRACT_OP, nil
				}
				return JSON_EXTRACT_OP, nil
			}
			return int(ch), nil
		case '<':
			switch tkn.lastChar {
			case '>':
				tkn.next()
				return NE, nil
			case '<':
				tkn.next()
				return SHIFT_LEFT, nil
			case '=':
				tkn.next()
				switch tkn.lastChar {
				case '>':
					tkn.next()
					return NULL_SAFE_EQUAL, nil
				default:
					return LE, nil
				}
			default:
				return int(ch), nil
			}
		case '>':
			switch tkn.lastChar {
			case '=':
				tkn.next()
				return GE, nil
			case '>':
				tkn.next()
				return SHIFT_RIGHT, nil
			default:
				return int(ch), nil
			}
		case '!':
			if tkn.mode == ParserModePostgres {
				if tkn.lastChar == '~' {
					tkn.next()
					if tkn.lastChar == '*' {
						tkn.next()
						return POSIX_NOT_REGEX_CI, nil
					}
					return POSIX_NOT_REGEX, nil
				}
			}
			if tkn.lastChar == '=' {
				tkn.next()
				return NE, nil
			}
			return int(ch), nil
		case '\'':
			return tkn.scanString(ch, STRING)
		case '"':
			if tkn.mode != ParserModeMysql {
				return tkn.scanLiteralIdentifier('"')
			} else {
				return tkn.scanString(ch, STRING)
			}
		default:
			if tkn.mode != ParserModePostgres && ch == '`' {
				return tkn.scanLiteralIdentifier('`')
			}
			return LEX_ERROR, []byte{byte(ch)}
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

func (tkn *Tokenizer) scanIdentifier(firstByte byte, isDbSystemVariable bool) (int, []byte) {
	buffer := &bytes2.Buffer{}
	buffer.WriteByte(firstByte)
	for isLetter(tkn.lastChar) || isDigit(tkn.lastChar) || (isDbSystemVariable && isCarat(tkn.lastChar)) {
		buffer.WriteByte(byte(tkn.lastChar))
		tkn.next()
	}
	lowered := bytes.ToLower(buffer.Bytes())
	loweredStr := string(lowered)
	if keywordID, found := keywords[loweredStr]; found {
		return keywordID, lowered
	}
	// dual must always be case-insensitive
	if loweredStr == "dual" {
		return ID, lowered
	}
	return ID, buffer.Bytes()
}

func (tkn *Tokenizer) scanHex() (int, []byte) {
	buffer := &bytes2.Buffer{}
	tkn.scanMantissa(16, buffer)
	if tkn.lastChar != '\'' {
		return LEX_ERROR, buffer.Bytes()
	}
	tkn.next()
	if buffer.Len()%2 != 0 {
		return LEX_ERROR, buffer.Bytes()
	}
	return HEX, buffer.Bytes()
}

func (tkn *Tokenizer) scanBitLiteral() (int, []byte) {
	buffer := &bytes2.Buffer{}
	tkn.scanMantissa(2, buffer)
	if tkn.lastChar != '\'' {
		return LEX_ERROR, buffer.Bytes()
	}
	tkn.next()
	return BIT_LITERAL, buffer.Bytes()
}

func (tkn *Tokenizer) scanLiteralIdentifier(sepChar uint16) (int, []byte) {
	buffer := &bytes2.Buffer{}
	backTickSeen := false
	for {
		if backTickSeen {
			if tkn.lastChar != sepChar {
				break
			}
			backTickSeen = false
			buffer.WriteByte(byte(sepChar))
			tkn.next()
			continue
		}
		// The previous char was not a backtick.
		switch tkn.lastChar {
		case sepChar:
			backTickSeen = true
		case eofChar:
			// Premature EOF.
			return LEX_ERROR, buffer.Bytes()
		default:
			buffer.WriteByte(byte(tkn.lastChar))
		}
		tkn.next()
	}
	if buffer.Len() == 0 {
		return LEX_ERROR, buffer.Bytes()
	}
	return ID, buffer.Bytes()
}

func (tkn *Tokenizer) scanBindVar() (int, []byte) {
	buffer := &bytes2.Buffer{}
	buffer.WriteByte(byte(tkn.lastChar))
	token := VALUE_ARG
	tkn.next()
	if tkn.lastChar == ':' {
		if tkn.mode == ParserModePostgres {
			buffer.WriteByte(byte(tkn.lastChar))
			tkn.next()
			return TYPECAST, buffer.Bytes()
		}
		token = LIST_ARG
		buffer.WriteByte(byte(tkn.lastChar))
		tkn.next()
	}
	if !isLetter(tkn.lastChar) {
		return LEX_ERROR, buffer.Bytes()
	}
	for isLetter(tkn.lastChar) || isDigit(tkn.lastChar) || tkn.lastChar == '.' {
		buffer.WriteByte(byte(tkn.lastChar))
		tkn.next()
	}
	return token, buffer.Bytes()
}

func (tkn *Tokenizer) scanMantissa(base int, buffer *bytes2.Buffer) {
	for digitVal(tkn.lastChar) < base {
		tkn.consumeNext(buffer)
	}
}

func (tkn *Tokenizer) scanNumber(seenDecimalPoint bool) (int, []byte) {
	token := INTEGRAL
	buffer := &bytes2.Buffer{}
	if seenDecimalPoint {
		token = FLOAT
		buffer.WriteByte('.')
		tkn.scanMantissa(10, buffer)
		goto exponent
	}

	// 0x construct.
	if tkn.lastChar == '0' {
		tkn.consumeNext(buffer)
		if tkn.lastChar == 'x' || tkn.lastChar == 'X' {
			token = HEXNUM
			tkn.consumeNext(buffer)
			tkn.scanMantissa(16, buffer)
			goto exit
		}
	}

	tkn.scanMantissa(10, buffer)

	if tkn.lastChar == '.' {
		token = FLOAT
		tkn.consumeNext(buffer)
		tkn.scanMantissa(10, buffer)
	}

exponent:
	if tkn.lastChar == 'e' || tkn.lastChar == 'E' {
		token = FLOAT
		tkn.consumeNext(buffer)
		if tkn.lastChar == '+' || tkn.lastChar == '-' {
			tkn.consumeNext(buffer)
		}
		tkn.scanMantissa(10, buffer)
	}

exit:
	// A letter cannot immediately follow a number.
	if isLetter(tkn.lastChar) {
		return LEX_ERROR, buffer.Bytes()
	}

	return token, buffer.Bytes()
}

func (tkn *Tokenizer) scanString(delim uint16, typ int) (int, []byte) {
	var buffer bytes2.Buffer
	for {
		ch := tkn.lastChar
		if ch == eofChar {
			// Unterminated string.
			return LEX_ERROR, buffer.Bytes()
		}

		if ch != delim && ch != '\\' {
			buffer.WriteByte(byte(ch))

			// Scan ahead to the next interesting character.
			start := tkn.bufPos
			for ; tkn.bufPos < tkn.bufSize; tkn.bufPos++ {
				ch = uint16(tkn.buf[tkn.bufPos])
				if ch == delim || ch == '\\' {
					break
				}
			}

			buffer.Write(tkn.buf[start:tkn.bufPos])
			tkn.Position += (tkn.bufPos - start)

			if tkn.bufPos >= tkn.bufSize {
				// Reached the end of the buffer without finding a delim or
				// escape character.
				tkn.next()
				continue
			}

			tkn.bufPos++
			tkn.Position++
		}
		tkn.next() // Read one past the delim or escape character.

		if ch == '\\' {
			if tkn.lastChar == eofChar {
				// String terminates mid escape character.
				return LEX_ERROR, buffer.Bytes()
			}
			if decodedChar := sqltypes.SQLDecodeMap[byte(tkn.lastChar)]; decodedChar == sqltypes.DontEscape {
				ch = tkn.lastChar
			} else {
				ch = uint16(decodedChar)
			}

		} else if ch == delim && tkn.lastChar != delim {
			// Correctly terminated string, which is not a double delim.
			break
		}

		buffer.WriteByte(byte(ch))
		tkn.next()
	}

	return typ, buffer.Bytes()
}

func (tkn *Tokenizer) scanCommentType1(prefix string) (int, []byte) {
	buffer := &bytes2.Buffer{}
	buffer.WriteString(prefix)
	for tkn.lastChar != eofChar {
		if tkn.lastChar == '\n' {
			tkn.consumeNext(buffer)
			break
		}
		tkn.consumeNext(buffer)
	}
	return COMMENT, buffer.Bytes()
}

func (tkn *Tokenizer) scanCommentType2() (int, []byte) {
	buffer := &bytes2.Buffer{}
	buffer.WriteString("/*")
	for {
		if tkn.lastChar == '*' {
			tkn.consumeNext(buffer)
			if tkn.lastChar == '/' {
				tkn.consumeNext(buffer)
				break
			}
			continue
		}
		if tkn.lastChar == eofChar {
			return LEX_ERROR, buffer.Bytes()
		}
		tkn.consumeNext(buffer)
	}
	return COMMENT, buffer.Bytes()
}

func (tkn *Tokenizer) scanMySQLSpecificComment() (int, []byte) {
	buffer := &bytes2.Buffer{}
	buffer.WriteString("/*!")
	tkn.next()
	for {
		if tkn.lastChar == '*' {
			tkn.consumeNext(buffer)
			if tkn.lastChar == '/' {
				tkn.consumeNext(buffer)
				break
			}
			continue
		}
		if tkn.lastChar == eofChar {
			return LEX_ERROR, buffer.Bytes()
		}
		tkn.consumeNext(buffer)
	}
	_, sql := ExtractMysqlComment(buffer.String())
	tkn.specialComment = NewStringTokenizer(sql, tkn.mode)
	return tkn.Scan()
}

func (tkn *Tokenizer) consumeNext(buffer *bytes2.Buffer) {
	if tkn.lastChar == eofChar {
		// This should never happen.
		panic("unexpected EOF")
	}
	buffer.WriteByte(byte(tkn.lastChar))
	tkn.next()
}

func (tkn *Tokenizer) next() {
	if tkn.bufPos >= tkn.bufSize && tkn.InStream != nil {
		// Try and refill the buffer
		var err error
		tkn.bufPos = 0
		if tkn.bufSize, err = tkn.InStream.Read(tkn.buf); err != io.EOF && err != nil {
			tkn.LastError = err
		}
	}

	if tkn.bufPos >= tkn.bufSize {
		if tkn.lastChar != eofChar {
			tkn.Position++
			tkn.lastChar = eofChar
		}
	} else {
		tkn.Position++
		tkn.lastChar = uint16(tkn.buf[tkn.bufPos])
		tkn.bufPos++
	}
}

// reset clears any internal state.
func (tkn *Tokenizer) reset() {
	tkn.ParseTree = nil
	tkn.partialDDL = nil
	tkn.specialComment = nil
	tkn.posVarIndex = 0
	tkn.nesting = 0
	tkn.ForceEOF = false
}

func isLetter(ch uint16) bool {
	return 'a' <= ch && ch <= 'z' || 'A' <= ch && ch <= 'Z' || ch == '_' || ch == '@'
}

func isCarat(ch uint16) bool {
	return ch == '.' || ch == '\'' || ch == '"' || ch == '`'
}

func digitVal(ch uint16) int {
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

func isDigit(ch uint16) bool {
	return '0' <= ch && ch <= '9'
}

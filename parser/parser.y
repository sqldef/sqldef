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

%{
package parser

import (
  "fmt"
  "strings"
)

func setParseTree(yylex interface{}, stmt Statement) {
  yylex.(*Tokenizer).ParseTree = stmt
}

func setAllowComments(yylex interface{}, allow bool) {
  yylex.(*Tokenizer).AllowComments = allow
}

func setDDL(yylex interface{}, ddl *DDL) {
  yylex.(*Tokenizer).partialDDL = ddl
}

func incNesting(yylex interface{}) bool {
  yylex.(*Tokenizer).nesting++
  if yylex.(*Tokenizer).nesting == 200 {
    return true
  }
  return false
}

func decNesting(yylex interface{}) {
  yylex.(*Tokenizer).nesting--
}

// forceEOF forces the lexer to end prematurely. Not all SQL statements
// are supported by the Parser, thus calling forceEOF will make the lexer
// return EOF early.
func forceEOF(yylex interface{}) {
  yylex.(*Tokenizer).ForceEOF = true
}

%}

%union {
  empty                    struct{}
  statement                Statement
  selStmt                  SelectStatement
  ddl                      *DDL
  ins                      *Insert
  byt                      byte
  bytes                    []byte
  bytes2                   [][]byte
  str                      string
  strs                     []string
  selectExprs              SelectExprs
  selectExpr               SelectExpr
  columns                  Columns
  partitions               Partitions
  colName                  *ColName
  newQualifierColName      *NewQualifierColName
  tableExprs               TableExprs
  tableExpr                TableExpr
  joinCondition            JoinCondition
  tableName                TableName
  tableNames               TableNames
  indexHints               *IndexHints
  expr                     Expr
  exprs                    Exprs
  boolVal                  BoolVal
  boolVals                 []BoolVal
  colTuple                 ColTuple
  values                   Values
  valTuple                 ValTuple
  subquery                 *Subquery
  whens                    []*When
  when                     *When
  orderBy                  OrderBy
  order                    *Order
  limit                    *Limit
  updateExprs              UpdateExprs
  setExprs                 SetExprs
  updateExpr               *UpdateExpr
  setExpr                  *SetExpr
  colIdent                 ColIdent
  colIdents                []ColIdent
  tableIdent               TableIdent
  convertType              *ConvertType
  aliasedTableName         *AliasedTableExpr
  TableSpec                *TableSpec
  columnType               ColumnType
  colKeyOpt                ColumnKeyOption
  optVal                   *SQLVal
  defaultValueOrExpression DefaultValueOrExpression
  LengthScaleOption        LengthScaleOption
  columnDefinition         *ColumnDefinition
  checkDefinition          *CheckDefinition
  exclusionDefinition      *ExclusionDefinition
  exclusionPair            ExclusionPair
  exclusionPairs           []ExclusionPair
  indexDefinition          *IndexDefinition
  indexInfo                *IndexInfo
  indexOption              *IndexOption
  indexOptions             []*IndexOption
  indexPartition           *IndexPartition
  indexColumn              IndexColumn
  indexColumns             []IndexColumn
  indexColumnsOrExpression IndexColumnsOrExpression
  foreignKeyDefinition     *ForeignKeyDefinition
  partDefs                 []*PartitionDefinition
  partDef                  *PartitionDefinition
  partSpec                 *PartitionSpec
  showFilter               *ShowFilter
  sequence                 *Sequence
  blockStatement           []Statement
  localVariable            *LocalVariable
  localVariables           []*LocalVariable
  arrayConstructor         *ArrayConstructor
  arrayElements            ArrayElements
  arrayElement             ArrayElement
  tableOptions             map[string]string
  overExpr                 *OverExpr
  partitionBy              PartitionBy
  partition                *Partition
}

%token LEX_ERROR
%left <bytes> UNION
%token <bytes> SELECT STREAM INSERT UPDATE DELETE FROM WHERE GROUP HAVING ORDER BY LIMIT OFFSET FOR DECLARE
%token <bytes> ALL ANY SOME DISTINCT AS EXISTS ASC DESC INTO DUPLICATE DEFAULT SRID SET LOCK KEYS
%token <bytes> ROWID STRICT
%token <bytes> VALUES LAST_INSERT_ID
%token <bytes> NEXT VALUE SHARE MODE
%token <bytes> SQL_NO_CACHE SQL_CACHE
%left <bytes> JOIN STRAIGHT_JOIN LEFT RIGHT INNER OUTER CROSS NATURAL USE FORCE
%left <bytes> ON USING
%token <empty> '(' ',' ')'
%token <bytes> ID HEX STRING UNICODE_STRING INTEGRAL FLOAT HEXNUM VALUE_ARG LIST_ARG COMMENT COMMENT_KEYWORD BIT_LITERAL
%token <bytes> NULL TRUE FALSE COLUMNS_UPDATED
%token <bytes> OFF
%token <bytes> MAX

// Precedence dictated by databases. But the sqldef grammar is simplified.
// Some of these operators don't conflict in our situation. Nevertheless,
// it's better to have these listed in the correct order. Also, we don't
// support all operators yet.
%left <bytes> OR
%left <bytes> AND
%right <bytes> NOT '!'
/* ---------------- Dangling Else Resolution --------------------------------
 * The following definitions, `NO_ELSE` and `ELSE`, are ordered specifically
 * to resolve the "dangling else" ambiguity. `ELSE` MUST have a higher
 * precedence than `NO_ELSE`. Their relative order and separation from
 * other keywords are critical for correct parsing.                           */
%nonassoc NO_ELSE
/** DO NOT MERGE these definitions. Their separation and order are critical. **/
%left <bytes> BETWEEN CASE WHEN THEN END
%left <bytes> ELSE
/* ---------------- End of Dangling Else Resolution ------------------------- */
%left <bytes> '=' '<' '>' LE GE NE NULL_SAFE_EQUAL IS LIKE REGEXP IN
%left <bytes> POSIX_REGEX POSIX_REGEX_CI POSIX_NOT_REGEX POSIX_NOT_REGEX_CI
%left <bytes> '|'
%left <bytes> '&'
%left <bytes> SHIFT_LEFT SHIFT_RIGHT
%left <bytes> '+' '-'
%left <bytes> '*' '/' DIV '%' MOD
%left <bytes> '^'
%right <bytes> '~' UNARY
%left <bytes> COLLATE
%right <bytes> BINARY UNDERSCORE_BINARY
%right <bytes> INTERVAL
%nonassoc <bytes> '.'

// There is no need to define precedence for the JSON
// operators because the syntax is restricted enough that
// they don't cause conflicts.
%token <empty> JSON_EXTRACT_OP JSON_UNQUOTE_EXTRACT_OP

// DDL Tokens
%token <bytes> CREATE ALTER DROP RENAME ANALYZE ADD GRANT REVOKE OPTION PRIVILEGES
%token <bytes> SCHEMA TABLE INDEX MATERIALIZED VIEW TO IGNORE IF PRIMARY COLUMN CONSTRAINT REFERENCES SPATIAL FULLTEXT FOREIGN KEY_BLOCK_SIZE POLICY WHILE EXCLUDE GIST
%right <bytes> UNIQUE KEY
%token <bytes> SHOW DESCRIBE EXPLAIN DATE ESCAPE REPAIR OPTIMIZE TRUNCATE EXEC EXECUTE
%token <bytes> MAXVALUE PARTITION REORGANIZE LESS THAN PROCEDURE TRIGGER TYPE RETURN
%token <bytes> EXTENSION DATA
%token <bytes> STATUS VARIABLES
%token <bytes> RESTRICT CASCADE NO ACTION
%token <bytes> PERMISSIVE RESTRICTIVE PUBLIC CURRENT_USER SESSION_USER
%token <bytes> PAD_INDEX FILLFACTOR IGNORE_DUP_KEY STATISTICS_NORECOMPUTE STATISTICS_INCREMENTAL ALLOW_ROW_LOCKS ALLOW_PAGE_LOCKS DISTANCE M EUCLIDEAN COSINE
%token <bytes> BEFORE AFTER EACH ROW SCROLL CURSOR OPEN CLOSE FETCH PRIOR FIRST LAST DEALLOCATE INSTEAD OF OUTPUT
%token <bytes> DEFERRABLE INITIALLY IMMEDIATE DEFERRED
%token <bytes> CONCURRENTLY
%token <bytes> SQL SECURITY

// Transaction Tokens
%token <bytes> BEGIN START TRANSACTION COMMIT ROLLBACK

// Type Tokens
%token <bytes> BIT TINYINT SMALLINT SMALLSERIAL MEDIUMINT INT INTEGER SERIAL BIGINT BIGSERIAL INTNUM
%token <bytes> REAL DOUBLE PRECISION FLOAT_TYPE DECIMAL NUMERIC SMALLMONEY MONEY
%token <bytes> TIME TIMESTAMP DATETIME YEAR DATETIMEOFFSET DATETIME2 SMALLDATETIME
%token <bytes> CHAR VARCHAR VARYING BOOL CHARACTER VARBINARY NCHAR NVARCHAR NTEXT UUID
%token <bytes> TEXT TINYTEXT MEDIUMTEXT LONGTEXT CITEXT
%token <bytes> BLOB TINYBLOB MEDIUMBLOB LONGBLOB JSON JSONB ENUM
%token <bytes> GEOMETRY POINT LINESTRING POLYGON GEOMETRYCOLLECTION MULTIPOINT MULTILINESTRING MULTIPOLYGON
%token <bytes> VECTOR
%token <bytes> VARIADIC ARRAY
%token <bytes> NOW GETDATE
%token <bytes> BPCHAR

// Operator Class Tokens
%right <bytes> TEXT_PATTERN_OPS

// Type Modifiers
%token <bytes> NULLX AUTO_INCREMENT APPROXNUM SIGNED UNSIGNED ZEROFILL ZONE AUTOINCREMENT

// Supported SHOW tokens
%token <bytes> DATABASES TABLES VSCHEMA_TABLES EXTENDED FULL PROCESSLIST

// SET tokens
%token <bytes> NAMES CHARSET GLOBAL SESSION ISOLATION LEVEL READ WRITE ONLY REPEATABLE COMMITTED UNCOMMITTED SERIALIZABLE NEW

// SET option tokens
%token <bytes> CONCAT_NULL_YIELDS_NULL CURSOR_CLOSE_ON_COMMIT QUOTED_IDENTIFIER ARITHABORT FMTONLY NOCOUNT NOEXEC
%token <bytes> NUMERIC_ROUNDABORT ANSI_DEFAULTS ANSI_NULL_DFLT_OFF ANSI_NULL_DFLT_ON ANSI_NULLS ANSI_PADDING ANSI_WARNINGS
%token <bytes> FORCEPLAN SHOWPLAN_ALL SHOWPLAN_TEXT SHOWPLAN_XML IMPLICIT_TRANSACTIONS REMOTE_PROC_TRANSACTIONS XACT_ABORT

// Functions
%token <bytes> CURRENT_TIMESTAMP DATABASE CURRENT_DATE
%token <bytes> CURRENT_TIME LOCALTIME LOCALTIMESTAMP
%token <bytes> UTC_DATE UTC_TIME UTC_TIMESTAMP
%token <bytes> REPLACE
%token <bytes> CONVERT CAST COALESCE
%token <bytes> SUBSTR SUBSTRING
%token <bytes> GROUP_CONCAT SEPARATOR
%token <bytes> INHERIT
%token <bytes> LEAD LAG

// Match
%token <bytes> MATCH AGAINST BOOLEAN LANGUAGE WITH WITHOUT PARSER QUERY EXPANSION

// MySQL reserved words that are unused by this grammar will map to this token.
%token <bytes> UNUSED

// MySQL PostgreSQL GENERATED ALWAYS AS
%token <bytes> VIRTUAL STORED
// PostgreSQL GENERATED AS IDENTITY
%token <bytes> GENERATED ALWAYS IDENTITY
// sequence
%token <bytes> SEQUENCE INCREMENT MINVALUE CACHE CYCLE OWNED NONE

// SQL Server PRIMARY KEY CLUSTERED
%token <bytes> CLUSTERED NONCLUSTERED
// SQL Server NOT FOR REPLICATION
%token <bytes> REPLICATION
// SQL SERVER COLUMNSTORE
%token <bytes> COLUMNSTORE
// index
%token <bytes> INCLUDE

// table hint
%token <bytes> HOLDLOCK NOLOCK NOWAIT PAGLOCK ROWLOCK TABLOCK UPDLOCK READUNCOMMITTED

// SQL SECURITY
%token <bytes> DEFINER INVOKER

%type <statement> statement
%type <selStmt> select_statement base_select union_lhs union_rhs
%type <statement> insert_statement update_statement delete_statement set_statement declare_statement cursor_statement while_statement exec_statement return_statement
%type <statement> if_statement matched_if_statement unmatched_if_statement trigger_statement_not_if
%type <blockStatement> simple_if_body
%type <statement> create_statement alter_statement comment_statement
%type <statement> set_option_statement set_bool_option_statement
%type <ddl> create_table_prefix
%type <bytes2> comment_opt comment_list
%type <str> union_op insert_or_replace exec_keyword
%type <str> distinct_opt straight_join_opt cache_opt match_option separator_opt
%type <expr> like_escape_opt
%type <selectExprs> select_expression_list select_expression_list_opt
%type <selectExpr> select_expression
%type <expr> expression
%type <tableExprs> from_opt table_references
%type <tableExpr> table_reference table_factor join_table
%type <joinCondition> join_condition join_condition_opt on_expression_opt
%type <tableNames> table_name_list
%type <str> inner_join outer_join straight_join natural_join
%type <tableName> table_name into_table_name
%type <aliasedTableName> aliased_table_name
%type <indexHints> index_hint_list
%type <expr> where_expression_opt
%type <expr> condition
%type <boolVal> boolean_value
%type <bytes> int_value
%type <str> compare
%type <ins> insert_data
%type <expr> value value_expression
%type <expr> function_call_keyword function_call_nonkeyword function_call_generic function_call_conflict
%type <str> is_suffix
%type <colTuple> col_tuple
%type <exprs> expression_list exec_param_list_opt
%type <values> tuple_list
%type <valTuple> row_tuple tuple_or_empty
%type <expr> tuple_expression
%type <subquery> subquery
%type <colName> column_name
%type <whens> when_expression_list
%type <when> when_expression
%type <expr> expression_opt else_expression_opt
%type <exprs> group_by_opt
%type <expr> having_opt
%type <orderBy> order_by_opt order_list
%type <order> order
%type <str> asc_desc_opt
%type <limit> limit_opt
%type <str> lock_opt
%type <columns> ins_column_list column_list
%type <colIdent> ins_column
%type <columns> include_columns_opt
%type <partitions> opt_partition_clause partition_list
%type <updateExprs> on_dup_opt
%type <updateExprs> update_list
%type <setExprs> set_list transaction_chars
%type <bytes> charset_or_character_set
%type <updateExpr> update_expression
%type <setExpr> set_expression transaction_char isolation_level
%type <str> ignore_opt default_opt
%type <empty> if_not_exists_opt when_expression_opt for_each_row_opt
%type <bytes> reserved_keyword non_reserved_keyword
%type <colIdent> sql_id reserved_sql_id col_alias as_ci_opt
%type <boolVal> unique_opt
%type <expr> charset_value
%type <tableIdent> table_id reserved_table_id table_alias as_opt_id
%type <empty> as_opt
%type <str> charset
%type <str> set_session_or_global
%type <convertType> convert_type simple_convert_type
%type <columnType> column_type
%type <columnType> bool_type int_type decimal_type numeric_type time_type char_type spatial_type
%type <str> precision_opt varying_opt
%type <optVal> length_opt max_length_opt current_timestamp
%type <str> charset_opt collate_opt
%type <boolVal> unsigned_opt zero_fill_opt array_opt time_zone_opt
%type <LengthScaleOption> float_length_opt decimal_length_opt
%type <strs> enum_values
%type <columnDefinition> column_definition
%type <columnType> column_definition_type
%type <indexDefinition> index_definition primary_key_definition unique_definition
%type <checkDefinition> check_definition
%type <exclusionDefinition> exclude_definition
%type <exclusionPair> exclude_element
%type <exclusionPairs> exclude_element_list
%type <foreignKeyDefinition> foreign_key_definition foreign_key_without_options
%type <colIdent> reference_option
%type <colIdent> sql_id_opt
%type <colIdents> sql_id_list
%type <str> index_or_key
%type <str> equal_opt
%type <TableSpec> table_spec table_column_list
%type <str> table_opt_name table_opt_value
%type <tableOptions> table_option_list
%type <indexInfo> index_info
%type <indexColumn> index_column
%type <bytes> operator_class
%type <indexColumnsOrExpression> index_column_list_or_expression
%type <indexColumns> index_column_list
%type <indexPartition> index_partition_opt
%type <indexOptions> index_option_opt
%type <indexOption> index_option
%type <indexOptions> index_option_list mssql_index_option_list
%type <bytes> policy_as_opt policy_for_opt
%type <expr> using_opt with_check_opt
%left <bytes> TYPECAST CHECK
%type <bytes> or_replace_opt
%type <boolVal> no_inherit_opt
%type <str> identity_behavior
%type <sequence> sequence_opt
%type <boolVal> clustered_opt not_for_replication_opt
%type <defaultValueOrExpression> default_definition
%type <optVal> srid_definition srid_val
%type <optVal> on_off
%type <optVal> index_distance_option_value
%type <optVal> vector_option_value
%type <str> trigger_time trigger_event fetch_opt
%type <strs> trigger_event_list
%type <blockStatement> trigger_statements statement_block
%type <statement> trigger_statement
%type <localVariable> local_variable
%type <localVariables> declare_variable_list
%type <boolVal> scroll_opt
%type <strs> table_hint_list table_hint_opt
%type <str> table_hint
%type <newQualifierColName> new_qualifier_column_name
%type <boolVal> deferrable_opt initially_deferred_opt
%type <arrayConstructor> array_constructor
%type <arrayElements> array_element_list
%type <arrayElement> array_element
%type <str> sql_security
%type <overExpr> over_expression
%token <bytes> OVER
%type <partitionBy> partition_by_list
%type <partition> partition
%type <boolVals> unique_clustered_opt
%type <empty> nonclustered_columnstore
%type <bytes> bool_option_name
%type <strs> bool_option_name_list
%type <str> grant_privilege_name
%type <strs> grant_privileges
%type <strs> grant_target_list

%start program

%%

program:
  statement semicolon_opt
  {
    setParseTree(yylex, $1)
  }

semicolon_opt:
/* empty */ {}
| ';' {}

statement:
  create_statement
| alter_statement
| comment_statement
  {
    $$ = $1
  }

create_statement:
  create_table_prefix table_spec
  {
    $1.TableSpec = $2
    $$ = $1
  }
| CREATE unique_clustered_opt INDEX sql_id ON table_name '(' index_column_list_or_expression ')' include_columns_opt where_expression_opt index_option_opt index_partition_opt
  {
    $$ = &DDL{
      Action: CreateIndex,
      Table: $6,
      NewName: $6,
      IndexSpec: &IndexSpec{
        Name: $4,
        Type: NewColIdent(""),
        Unique: bool($2[0]),
        Clustered: bool($2[1]),
        Included: $10,
        Where: NewWhere(WhereStr, $11),
        Options: $12,
        Partition: $13,
      },
      IndexCols: $8.IndexCols,
      IndexExpr: $8.IndexExpr,
    }
  }
| CREATE unique_clustered_opt INDEX ON table_name '(' index_column_list_or_expression ')' include_columns_opt where_expression_opt index_option_opt index_partition_opt
  {
    $$ = &DDL{
      Action: CreateIndex,
      Table: $5,
      NewName: $5,
      IndexSpec: &IndexSpec{
        Name: NewColIdent(""),
        Type: NewColIdent(""),
        Unique: bool($2[0]),
        Clustered: bool($2[1]),
        Included: $9,
        Where: NewWhere(WhereStr, $10),
        Options: $11,
        Partition: $12,
      },
      IndexCols: $7.IndexCols,
      IndexExpr: $7.IndexExpr,
    }
  }
| CREATE unique_clustered_opt INDEX CONCURRENTLY sql_id ON table_name '(' index_column_list_or_expression ')' include_columns_opt where_expression_opt index_option_opt index_partition_opt
  {
    $$ = &DDL{
      Action: CreateIndex,
      Table: $7,
      NewName: $7,
      IndexSpec: &IndexSpec{
        Name: $5,
        Type: NewColIdent(""),
        Unique: bool($2[0]),
        Clustered: bool($2[1]),
        Included: $11,
        Where: NewWhere(WhereStr, $12),
        Options: $13,
        Partition: $14,
      },
      IndexCols: $9.IndexCols,
      IndexExpr: $9.IndexExpr,
    }
  }
/* For MySQL */
| CREATE unique_clustered_opt INDEX sql_id USING sql_id ON table_name '(' index_column_list ')' index_option_opt
  {
    $$ = &DDL{
      Action: CreateIndex,
      Table: $8,
      NewName: $8,
      IndexSpec: &IndexSpec{
        Name: $4,
        Type: $6,
        Unique: bool($2[0]),
        Options: $12,
      },
      IndexCols: $10,
    }
  }
/* For PostgreSQL */
| CREATE unique_clustered_opt INDEX sql_id ON table_name USING sql_id '(' index_column_list_or_expression ')' where_expression_opt index_option_opt
  {
    $$ = &DDL{
      Action: CreateIndex,
      Table: $6,
      NewName: $6,
      IndexSpec: &IndexSpec{
        Name: $4,
        Type: $8,
        Unique: bool($2[0]),
        Where: NewWhere(WhereStr, $12),
      },
      IndexCols: $10.IndexCols,
      IndexExpr: $10.IndexExpr,
    }
  }
/* For SQL Server */
| CREATE nonclustered_columnstore INDEX sql_id ON table_name '(' column_list ')' where_expression_opt index_option_opt index_partition_opt
  {
    $$ = &DDL{
      Action: CreateIndex,
      Table: $6,
      NewName: $6,
      IndexSpec: &IndexSpec{
        Name: $4,
        Type: NewColIdent(""),
        Unique: false,
        Clustered: false,
        ColumnStore: true,
        Included: $8,
        Where: NewWhere(WhereStr, $10),
        Options: $11,
        Partition: $12,
      },
    }
  }
/* For MariaDB */
| CREATE VECTOR INDEX sql_id ON table_name '(' index_column_list ')' index_option_opt
  {
    $$ = &DDL{
      Action: CreateIndex,
      Table: $6,
      NewName: $6,
      IndexSpec: &IndexSpec{
        Name: $4,
        Type: NewColIdent("VECTOR"),
        Vector: true,
        Options: $10,
      },
      IndexCols: $8,
    }
  }
| CREATE or_replace_opt VIEW if_not_exists_opt table_name AS select_statement
  {
    $$ = &DDL{
      Action: CreateView,
      View: &View{
        Type: ViewStr,
        Name: $5.toViewName(),
        Definition: $7,
      },
    }
  }
| CREATE or_replace_opt SQL SECURITY sql_security VIEW if_not_exists_opt table_name AS select_statement
  {
    $$ = &DDL{
      Action: CreateView,
      View: &View{
        Type: SqlSecurityStr,
        SecurityType: $5,
        Name: $8.toViewName(),
        Definition: $10,
      },
    }
  }
| CREATE MATERIALIZED VIEW if_not_exists_opt table_name AS select_statement
  {
    $$ = &DDL{
      Action: CreateView,
      View: &View{
        Type: MaterializedViewStr,
        Name: $5.toViewName(),
        Definition: $7,
      },
    }
  }
| CREATE POLICY sql_id ON table_name policy_as_opt policy_for_opt TO sql_id_list using_opt with_check_opt
  {
    $$ = &DDL{
      Action: CreatePolicy,
      Table: $5,
      Policy: &Policy{
        Name: $3,
        Permissive: Permissive($6),
        Scope: $7,
        To: $9,
        Using: NewWhere(WhereStr, $10),
        WithCheck: NewWhere(WhereStr, $11),
      },
    }
  }
/* For MySQL */
| CREATE TRIGGER sql_id trigger_time trigger_event_list ON table_name FOR EACH ROW trigger_statements
  {
    $$ = &DDL{
      Action: CreateTrigger,
      Trigger: &Trigger{
        Name: &ColName{Name: $3},
        TableName: $7,
        Time: $4,
        Event: $5,
        Body: $11,
      },
    }
  }
/* For MSSQL */
| CREATE TRIGGER column_name ON table_name trigger_time trigger_event_list AS trigger_statements
  {
    $$ = &DDL{
      Action: CreateTrigger,
      Trigger: &Trigger{
        Name: $3,
        TableName: $5,
        Time: $6,
        Event: $7,
        Body: $9,
      },
    }
  }
/* For SQLite3 */
| CREATE TRIGGER sql_id trigger_time trigger_event_list ON table_name for_each_row_opt when_expression_opt BEGIN statement_block ';' END
  {
    $$ = &DDL{
      Action: CreateTrigger,
      Trigger: &Trigger{
        Name: &ColName{Name: $3},
        TableName: $7,
        Time: $4,
        Event: $5,
        Body: $11,
      },
    }
  }
| CREATE TRIGGER if_not_exists_opt sql_id trigger_time trigger_event_list ON table_name for_each_row_opt when_expression_opt BEGIN statement_block ';' END
  {
    $$ = &DDL{
      Action: CreateTrigger,
      Trigger: &Trigger{
        Name: &ColName{Name: $4},
        TableName: $8,
        Time: $5,
        Event: $6,
        Body: $12,
      },
    }
  }
/* For PostgreSQL */
| CREATE TYPE table_name AS column_type
  {
    $$ = &DDL{
      Action: CreateType,
      Type: &Type{
        Name: $3,
        Type: $5,
      },
    }
  }
/* For PostgreSQL - CREATE TYPE AS ENUM */
| CREATE TYPE table_name AS ENUM '(' enum_values ')'
  {
    $$ = &DDL{
      Action: CreateType,
      Type: &Type{
        Name: $3,
        Type: ColumnType{Type: "enum", EnumValues: $7},
      },
    }
  }
/* For PostgreSQL */
| CREATE EXTENSION if_not_exists_opt reserved_sql_id
  {
    $$ = &DDL{
      Action: CreateExtension,
      Extension: &Extension{
        Name: $4.String(),
      },
    }
  }
/* For SQLite3, only to parse because alternation is not supported. // The Virtual Table Mechanism Of SQLite https://www.sqlite.org/vtab.html */
| CREATE VIRTUAL TABLE if_not_exists_opt table_name USING sql_id module_arguments_opt
  {
    $$ = &DDL{Action: CreateTable, NewName: $5, TableSpec: &TableSpec{}}
  }
| CREATE SCHEMA if_not_exists_opt sql_id
  {
    $$ = &DDL{
      Action: CreateSchema,
      Schema: &Schema{
        Name: $4.String(),
      },
    }
  }
| GRANT grant_privileges ON TABLE table_name_list TO grant_target_list
  {
    // Convert table names to multiple Grant statements (one per table)
    var stmts []Statement
    for _, tableName := range $5 {
      stmts = append(stmts, &DDL{
        Action: GrantPrivilege,
        Grant: &Grant{
          IsGrant:    true,
          Privileges: $2,
          TableName:  tableName,
          Grantees:   $7,
        },
      })
    }
    if len(stmts) == 1 {
      $$ = stmts[0]
    } else {
      $$ = &MultiStatement{Statements: stmts}
    }
  }
| GRANT grant_privileges ON TABLE table_name_list TO grant_target_list WITH GRANT OPTION
  {
    // Convert table names to multiple Grant statements (one per table)
    var stmts []Statement
    for _, tableName := range $5 {
      stmts = append(stmts, &DDL{
        Action: GrantPrivilege,
        Grant: &Grant{
          IsGrant:         true,
          Privileges:      $2,
          TableName:       tableName,
          Grantees:        $7,
          WithGrantOption: true,
        },
      })
    }
    if len(stmts) == 1 {
      $$ = stmts[0]
    } else {
      $$ = &MultiStatement{Statements: stmts}
    }
  }
| REVOKE grant_privileges ON TABLE table_name_list FROM grant_target_list
  {
    // Convert table names to multiple Revoke statements (one per table)
    var stmts []Statement
    for _, tableName := range $5 {
      stmts = append(stmts, &DDL{
        Action: RevokePrivilege,
        Grant: &Grant{
          IsGrant:    false,
          Privileges: $2,
          TableName:  tableName,
          Grantees:   $7,
        },
      })
    }
    if len(stmts) == 1 {
      $$ = stmts[0]
    } else {
      $$ = &MultiStatement{Statements: stmts}
    }
  }
| REVOKE grant_privileges ON TABLE table_name_list FROM grant_target_list CASCADE
  {
    // Convert table names to multiple Revoke statements (one per table)
    var stmts []Statement
    for _, tableName := range $5 {
      stmts = append(stmts, &DDL{
        Action: RevokePrivilege,
        Grant: &Grant{
          IsGrant:       false,
          Privileges:    $2,
          TableName:     tableName,
          Grantees:      $7,
          CascadeOption: true,
        },
      })
    }
    if len(stmts) == 1 {
      $$ = stmts[0]
    } else {
      $$ = &MultiStatement{Statements: stmts}
    }
  }

alter_statement:
  ALTER TABLE table_name ADD COLUMN column_definition
  {
    $$ = nil
  }
| ALTER TABLE table_name ALTER COLUMN sql_id SET DEFAULT value_expression
  {
    $$ = nil
  }
| ALTER TABLE table_name ALTER COLUMN sql_id DROP DEFAULT
  {
    $$ = nil
  }
| ALTER TABLE table_name ALTER COLUMN sql_id SET NOT NULL
  {
    $$ = nil
  }
| ALTER TABLE table_name ALTER COLUMN sql_id DROP NOT NULL
  {
    $$ = nil
  }
| ALTER TABLE table_name ALTER COLUMN sql_id TYPE column_type
  {
    $$ = nil
  }
| ALTER TABLE table_name ALTER COLUMN sql_id SET DATA TYPE column_type
  {
    $$ = nil
  }
/* ADD INDEX/KEY rules must come before ADD column rules to avoid ambiguity */
| ALTER TABLE table_name ADD unique_opt alter_object_type_index sql_id '(' index_column_list ')'
  {
    $$ = &DDL{
      Action: AddIndex,
      Table: $3,
      NewName: $3,
      IndexSpec: &IndexSpec{
        Name: $7,
        Unique: bool($5),
        Primary: false,
      },
      IndexCols: $9,
    }
  }
| ALTER ignore_opt TABLE table_name ADD unique_opt alter_object_type_index sql_id '(' index_column_list ')'
  {
    $$ = &DDL{
      Action: AddIndex,
      Table: $4,
      NewName: $4,
      IndexSpec: &IndexSpec{
        Name: $8,
        Unique: bool($6),
        Primary: false,
      },
      IndexCols: $10,
    }
  }
| ALTER ignore_opt TABLE table_name ADD COLUMN column_definition
  {
    $$ = nil
  }
| ALTER ignore_opt TABLE table_name ADD column_definition
  {
    $$ = nil
  }
| ALTER ignore_opt TABLE table_name ALTER COLUMN sql_id SET DEFAULT value_expression
  {
    $$ = nil
  }
| ALTER ignore_opt TABLE table_name ALTER COLUMN sql_id DROP DEFAULT
  {
    $$ = nil
  }
| ALTER ignore_opt TABLE table_name ALTER COLUMN sql_id SET NOT NULL
  {
    $$ = nil
  }
| ALTER ignore_opt TABLE table_name ALTER COLUMN sql_id DROP NOT NULL
  {
    $$ = nil
  }
| ALTER ignore_opt TABLE table_name ALTER COLUMN sql_id TYPE column_type
  {
    $$ = nil
  }
| ALTER ignore_opt TABLE table_name ALTER COLUMN sql_id SET DATA TYPE column_type
  {
    $$ = nil
  }
| ALTER TABLE table_name ADD CONSTRAINT sql_id UNIQUE '(' index_column_list ')' deferrable_opt initially_deferred_opt
  {
    $$ = &DDL{
      Action: AddIndex,
      Table: $3,
      NewName: $3,
      IndexSpec: &IndexSpec{
        Name: $6,
        Unique: true,
        Primary: false,
        Constraint: true,
        ConstraintOptions: &ConstraintOptions{
          Deferrable: bool($11),
          InitiallyDeferred: bool($12),
        },
      },
      IndexCols: $9,
    }
  }
| ALTER TABLE table_name ADD CONSTRAINT sql_id UNIQUE CLUSTERED '(' index_column_list ')' index_option_opt index_partition_opt
  {
    $$ = &DDL{
      Action: AddIndex,
      Table: $3,
      NewName: $3,
      IndexSpec: &IndexSpec{
        Name: $6,
        Unique: true,
        Primary: false,
        Clustered: true,
        Constraint: true,
        Options: $12,
        Partition: $13,
      },
      IndexCols: $10,
    }
  }
| ALTER TABLE table_name ADD CONSTRAINT sql_id UNIQUE NONCLUSTERED '(' index_column_list ')' index_option_opt index_partition_opt
  {
    $$ = &DDL{
      Action: AddIndex,
      Table: $3,
      NewName: $3,
      IndexSpec: &IndexSpec{
        Name: $6,
        Unique: true,
        Primary: false,
        Clustered: false,
        Constraint: true,
        Options: $12,
        Partition: $13,
      },
      IndexCols: $10,
    }
  }
| ALTER TABLE table_name ADD foreign_key_definition
  {
    $$ = &DDL{
      Action: AddForeignKey,
      Table: $3,
      NewName: $3,
      ForeignKey: $5,
    }
  }
| ALTER TABLE ONLY table_name ADD CONSTRAINT sql_id PRIMARY KEY '(' index_column_list ')'
  {
    $$ = &DDL{
      Action: AddPrimaryKey,
      Table: $4,
      NewName: $4,
      IndexSpec: &IndexSpec{
        Name: $7,
        Unique: false,
        Primary: true,
      },
      IndexCols: $11,
    }
  }
| ALTER TABLE table_name ADD CONSTRAINT sql_id PRIMARY KEY '(' index_column_list ')'
  {
    $$ = &DDL{
      Action: AddPrimaryKey,
      Table: $3,
      NewName: $3,
      IndexSpec: &IndexSpec{
        Name: $6,
        Unique: false,
        Primary: true,
      },
      IndexCols: $10,
    }
  }
| ALTER TABLE table_name ADD PRIMARY KEY '(' index_column_list ')'
  {
    $$ = &DDL{
      Action: AddPrimaryKey,
      Table: $3,
      NewName: $3,
      IndexSpec: &IndexSpec{
        Name: NewColIdent(""),
        Unique: false,
        Primary: true,
      },
      IndexCols: $8,
    }
  }
| ALTER TABLE ONLY table_name ADD foreign_key_definition
  {
    $$ = &DDL{
      Action: AddForeignKey,
      Table: $4,
      NewName: $4,
      ForeignKey: $6,
    }
  }
| ALTER ignore_opt TABLE ONLY table_name ADD CONSTRAINT sql_id PRIMARY KEY '(' index_column_list ')'
  {
    $$ = &DDL{
      Action: AddPrimaryKey,
      Table: $5,
      NewName: $5,
      IndexSpec: &IndexSpec{
        Name: $8,
        Unique: false,
        Primary: true,
      },
      IndexCols: $12,
    }
  }
| ALTER ignore_opt TABLE table_name ADD CONSTRAINT sql_id UNIQUE '(' index_column_list ')' deferrable_opt initially_deferred_opt
  {
    $$ = &DDL{
      Action: AddIndex,
      Table: $4,
      NewName: $4,
      IndexSpec: &IndexSpec{
        Name: $7,
        Unique: true,
        Primary: false,
        Constraint: true,
        ConstraintOptions: &ConstraintOptions{
          Deferrable: bool($12),
          InitiallyDeferred: bool($13),
        },
      },
      IndexCols: $10,
    }
  }
/* For SQL Server */
| ALTER ignore_opt TABLE table_name ADD CONSTRAINT sql_id UNIQUE CLUSTERED '(' index_column_list ')' index_option_opt index_partition_opt
  {
    $$ = &DDL{
      Action: AddIndex,
      Table: $4,
      NewName: $4,
      IndexSpec: &IndexSpec{
        Name: $7,
        Unique: true,
        Primary: false,
        Constraint: true,
        Clustered: true,
        Options: $13,
        Partition: $14,
      },
      IndexCols: $11,
    }
  }
| ALTER ignore_opt TABLE table_name ADD CONSTRAINT sql_id UNIQUE NONCLUSTERED '(' index_column_list ')' index_option_opt index_partition_opt
  {
    $$ = &DDL{
      Action: AddIndex,
      Table: $4,
      NewName: $4,
      IndexSpec: &IndexSpec{
        Name: $7,
        Unique: true,
        Primary: false,
        Constraint: true,
        Clustered: false,
        Options: $13,
        Partition: $14,
      },
      IndexCols: $11,
    }
  }
| ALTER ignore_opt TABLE table_name ADD foreign_key_definition
  {
    $$ = &DDL{
      Action: AddForeignKey,
      Table: $4,
      NewName: $4,
      ForeignKey: $6,
    }
  }
| ALTER ignore_opt TABLE ONLY table_name ADD foreign_key_definition
  {
    $$ = &DDL{
      Action: AddForeignKey,
      Table: $5,
      NewName: $5,
      ForeignKey: $7,
    }
  }
| ALTER TABLE table_name DROP COLUMN sql_id
  {
    $$ = nil
  }
| ALTER TABLE table_name DROP CONSTRAINT sql_id
  {
    $$ = nil
  }
| ALTER TABLE table_name ADD CONSTRAINT sql_id CHECK '(' expression ')'
  {
    $$ = nil
  }
| ALTER TABLE table_name ADD CONSTRAINT sql_id CHECK '(' expression ')' NO INHERIT
  {
    $$ = nil
  }
| ALTER TABLE table_name ADD CONSTRAINT sql_id EXCLUDE '(' exclude_element_list ')'
  {
    $$ = &DDL{
      Action: AddExclusion,
      Table: $3,
      NewName: $3,
      Exclusion: &ExclusionDefinition{
        ConstraintName: $6,
        Exclusions: $9,
      },
    }
  }
| ALTER TABLE table_name ADD CONSTRAINT sql_id EXCLUDE '(' exclude_element_list ')' where_expression_opt
  {
    $$ = &DDL{
      Action: AddExclusion,
      Table: $3,
      NewName: $3,
      Exclusion: &ExclusionDefinition{
        ConstraintName: $6,
        Exclusions: $9,
        Where: NewWhere(WhereStr, $11),
      },
    }
  }
| ALTER TABLE table_name ADD CONSTRAINT sql_id EXCLUDE USING GIST '(' exclude_element_list ')'
  {
    $$ = &DDL{
      Action: AddExclusion,
      Table: $3,
      NewName: $3,
      Exclusion: &ExclusionDefinition{
        ConstraintName: $6,
        IndexType: "GIST",
        Exclusions: $11,
      },
    }
  }
| ALTER TABLE table_name ADD CONSTRAINT sql_id EXCLUDE USING GIST '(' exclude_element_list ')' where_expression_opt
  {
    $$ = &DDL{
      Action: AddExclusion,
      Table: $3,
      NewName: $3,
      Exclusion: &ExclusionDefinition{
        ConstraintName: $6,
        IndexType: "GIST",
        Exclusions: $11,
        Where: NewWhere(WhereStr, $13),
      },
    }
  }
| ALTER TABLE table_name ADD CONSTRAINT sql_id EXCLUDE USING sql_id '(' exclude_element_list ')'
  {
    $$ = &DDL{
      Action: AddExclusion,
      Table: $3,
      NewName: $3,
      Exclusion: &ExclusionDefinition{
        ConstraintName: $6,
        IndexType: $9.String(),
        Exclusions: $11,
      },
    }
  }
| ALTER TABLE table_name ADD CONSTRAINT sql_id EXCLUDE USING sql_id '(' exclude_element_list ')' where_expression_opt
  {
    $$ = &DDL{
      Action: AddExclusion,
      Table: $3,
      NewName: $3,
      Exclusion: &ExclusionDefinition{
        ConstraintName: $6,
        IndexType: $9.String(),
        Exclusions: $11,
        Where: NewWhere(WhereStr, $13),
      },
    }
  }
| ALTER ignore_opt TABLE table_name DROP COLUMN sql_id
  {
    $$ = nil
  }
| ALTER ignore_opt TABLE table_name DROP CONSTRAINT sql_id
  {
    $$ = nil
  }
| ALTER ignore_opt TABLE table_name ADD CONSTRAINT sql_id CHECK '(' expression ')'
  {
    $$ = nil
  }
| ALTER ignore_opt TABLE table_name ADD CONSTRAINT sql_id CHECK '(' expression ')' NO INHERIT
  {
    $$ = nil
  }
| ALTER INDEX table_name RENAME TO sql_id
  {
    $$ = nil
  }
| ALTER TABLE table_name RENAME TO sql_id
  {
    $$ = nil
  }
| ALTER TABLE table_name RENAME COLUMN sql_id TO sql_id
  {
    $$ = nil
  }
| ALTER TYPE table_name ADD VALUE if_not_exists_opt STRING
  {
    $$ = nil
  }

comment_statement:
  COMMENT_KEYWORD ON TABLE table_name IS STRING
  {
    $$ = &DDL{
      Action: CommentOn,
      Comment: &Comment{
        ObjectType: "TABLE",
        Object:     String($4),
        Comment:    string($6),
      },
    }
  }
| COMMENT_KEYWORD ON TABLE table_name IS NULL
  {
    $$ = &DDL{
      Action: CommentOn,
      Comment: &Comment{
        ObjectType: "TABLE",
        Object:     String($4),
        Comment:    "NULL",
      },
    }
  }
| COMMENT_KEYWORD ON COLUMN table_id '.' sql_id IS STRING
  {
    $$ = &DDL{
      Action: CommentOn,
      Comment: &Comment{
        ObjectType: "COLUMN",
        Object:     $4.String() + "." + $6.String(),
        Comment:    string($8),
      },
    }
  }
| COMMENT_KEYWORD ON COLUMN table_id '.' sql_id IS NULL
  {
    $$ = &DDL{
      Action: CommentOn,
      Comment: &Comment{
        ObjectType: "COLUMN",
        Object:     $4.String() + "." + $6.String(),
        Comment:    "NULL",
      },
    }
  }
| COMMENT_KEYWORD ON COLUMN table_id '.' reserved_table_id '.' sql_id IS STRING
  {
    $$ = &DDL{
      Action: CommentOn,
      Comment: &Comment{
        ObjectType: "COLUMN",
        Object:     $4.String() + "." + $6.String() + "." + $8.String(),
        Comment:    string($10),
      },
    }
  }
| COMMENT_KEYWORD ON COLUMN table_id '.' reserved_table_id '.' sql_id IS NULL
  {
    $$ = &DDL{
      Action: CommentOn,
      Comment: &Comment{
        ObjectType: "COLUMN",
        Object:     $4.String() + "." + $6.String() + "." + $8.String(),
        Comment:    "NULL",
      },
    }
  }

alter_object_type_index:
  INDEX
| KEY

select_statement:
  base_select order_by_opt limit_opt lock_opt
  {
    sel := $1.(*Select)
    sel.OrderBy = $2
    sel.Limit = $3
    sel.Lock = $4
    $$ = sel
  }
| union_lhs union_op union_rhs order_by_opt limit_opt lock_opt
  {
    $$ = &Union{Type: $2, Left: $1, Right: $3, OrderBy: $4, Limit: $5, Lock: $6}
  }

// base_select is an unparenthesized SELECT with no order by clause or beyond.
base_select:
  SELECT comment_opt cache_opt distinct_opt straight_join_opt select_expression_list from_opt where_expression_opt group_by_opt having_opt
  {
    $$ = &Select{Comments: Comments($2), Cache: $3, Distinct: $4, Hints: $5, SelectExprs: $6, From: $7, Where: NewWhere(WhereStr, $8), GroupBy: GroupBy($9), Having: NewWhere(HavingStr, $10)}
  }

union_lhs:
  select_statement
  {
    $$ = $1
  }
| openb select_statement closeb
  {
    $$ = &ParenSelect{Select: $2}
  }

union_rhs:
  base_select
  {
    $$ = $1
  }
| openb select_statement closeb
  {
    $$ = &ParenSelect{Select: $2}
  }


insert_statement:
  insert_or_replace comment_opt ignore_opt into_table_name opt_partition_clause insert_data on_dup_opt
  {
    // insert_data returns a *Insert pre-filled with Columns & Values
    ins := $6
    ins.Action = $1
    ins.Comments = $2
    ins.Ignore = $3
    ins.Table = $4
    ins.Partitions = $5
    ins.OnDup = OnDup($7)
    $$ = ins
  }
| insert_or_replace comment_opt ignore_opt into_table_name opt_partition_clause SET update_list on_dup_opt
  {
    cols := make(Columns, 0, len($7))
    vals := make(ValTuple, 0, len($8))
    for _, updateList := range $7 {
      cols = append(cols, updateList.Name.Name)
      vals = append(vals, updateList.Expr)
    }
    $$ = &Insert{Action: $1, Comments: Comments($2), Ignore: $3, Table: $4, Partitions: $5, Columns: cols, Rows: Values{vals}, OnDup: OnDup($8)}
  }

insert_or_replace:
  INSERT
  {
    $$ = InsertStr
  }
| REPLACE
  {
    $$ = ReplaceStr
  }

update_statement:
  UPDATE comment_opt table_references SET update_list from_opt where_expression_opt order_by_opt limit_opt
  {
    $$ = &Update{Comments: Comments($2), TableExprs: $3, Exprs: $5, From: $6, Where: NewWhere(WhereStr, $7), OrderBy: $8, Limit: $9}
  }

delete_statement:
  DELETE comment_opt FROM table_name opt_partition_clause where_expression_opt order_by_opt limit_opt
  {
    $$ = &Delete{Comments: Comments($2), TableExprs:  TableExprs{&AliasedTableExpr{Expr:$4}}, Partitions: $5, Where: NewWhere(WhereStr, $6), OrderBy: $7, Limit: $8}
  }
| DELETE comment_opt FROM table_name_list USING table_references where_expression_opt
  {
    $$ = &Delete{Comments: Comments($2), Targets: $4, TableExprs: $6, Where: NewWhere(WhereStr, $7)}
  }
| DELETE comment_opt table_name_list from_or_using table_references where_expression_opt
  {
    $$ = &Delete{Comments: Comments($2), Targets: $3, TableExprs: $5, Where: NewWhere(WhereStr, $6)}
  }

from_or_using:
  FROM {}
| USING {}

table_name_list:
  table_name
  {
    $$ = TableNames{$1}
  }
| table_name_list ',' table_name
  {
    $$ = append($$, $3)
  }

opt_partition_clause:
  {
    $$ = nil
  }
| PARTITION openb partition_list closeb
  {
    $$ = $3
  }

set_statement:
  SET comment_opt set_list
  {
    $$ = &Set{Comments: Comments($2), Exprs: $3}
  }
| SET comment_opt set_session_or_global set_list
  {
    $$ = &Set{Comments: Comments($2), Scope: $3, Exprs: $4}
  }
| SET comment_opt set_session_or_global TRANSACTION transaction_chars
  {
    $$ = &Set{Comments: Comments($2), Scope: $3, Exprs: $5}
  }
| SET comment_opt TRANSACTION transaction_chars
  {
    $$ = &Set{Comments: Comments($2), Exprs: $4}
  }

declare_statement:
  DECLARE declare_variable_list
  {
    $$ = &Declare{Type: declareVariable, Variables: $2}
  }
| DECLARE sql_id scroll_opt CURSOR FOR select_statement
  {
    $$ = &Declare{
      Type: declareCursor,
      Cursor: &CursorDefinition{
        Name: $2,
        Scroll: bool($3),
        Select: $6,
      },
    }
  }

declare_variable_list:
  local_variable
  {
    $$ = []*LocalVariable{$1}
  }
| declare_variable_list ',' local_variable
  {
    $$ = append($$, $3)
  }

local_variable:
  sql_id as_opt column_type
  {
    $$ = &LocalVariable{Name: $1, DataType: $3}
  }

scroll_opt:
  {
    $$ = BoolVal(false)
  }
| SCROLL
  {
    $$ = BoolVal(true)
  }

cursor_statement:
  OPEN sql_id
  {
    $$ = &Cursor{
      Action: OpenStr,
      CursorName: $2,
    }
  }
| CLOSE sql_id
  {
    $$ = &Cursor{
      Action: CloseStr,
      CursorName: $2,
    }
  }
| DEALLOCATE sql_id
  {
    $$ = &Cursor{
      Action: DeallocateStr,
      CursorName: $2,
    }
  }
| FETCH fetch_opt sql_id
  {
    $$ = &Cursor{
      Action: FetchStr,
      Fetch: $2,
      CursorName: $3,
    }
  }
| FETCH fetch_opt sql_id INTO sql_id_list
  {
    $$ = &Cursor{
      Action: FetchStr,
      Fetch: $2,
      CursorName: $3,
      Into: $5,
    }
  }

fetch_opt:
  {
    $$ = ""
  }
| NEXT FROM
  {
    $$ = string($1)
  }
| PRIOR FROM
  {
    $$ = string($1)
  }
| FIRST FROM
  {
    $$ = string($1)
  }
| LAST FROM
  {
    $$ = string($1)
  }

while_statement:
  WHILE expression trigger_statement
  {
    $$ = &While{
      Condition: $2,
      Statements: []Statement{$3},
    }
  }
| WHILE expression BEGIN statement_block END
  {
    $$ = &While{
      Condition: $2,
      Statements: []Statement{
        &BeginEnd{
          Statements: $4,
          SuppressSemicolon: true,
        },
      },
    }
  }

statement_block:
  trigger_statement
  {
    $$ = []Statement{$1}
  }
| statement_block trigger_statement
  {
    $$ = append($$, $2)
  }
| statement_block ';' trigger_statement
  {
    $$ = append($$, $3)
  }

if_statement:
  // For MySQL
  IF expression THEN trigger_statements ';' END IF
  {
    $$ = &If{
      Condition: $2,
      IfStatements: $4,
      Keyword: string($3),
    }
  }
| IF expression THEN trigger_statements ';' ELSE trigger_statements ';' END IF
  {
    $$ = &If{
      Condition: $2,
      IfStatements: $4,
      ElseStatements: $7,
      Keyword: string($3),
    }
  }
// For MSSQL: Decompose into matched and unmatched statements to resolve ambiguity
| matched_if_statement
  {
    $$ = $1
  }
| unmatched_if_statement
  {
    $$ = $1
  }

matched_if_statement:
  // Recursive rule for 'ELSE IF' chains
  IF expression simple_if_body ELSE matched_if_statement
  {
    $$ = &If{
      Condition: $2,
      IfStatements: $3,
      ElseStatements: []Statement{$5},
      Keyword: "Mssql",
    }
  }
  // Base case rule for a simple, final 'ELSE'
| IF expression simple_if_body ELSE simple_if_body
  {
    $$ = &If{
      Condition: $2,
      IfStatements: $3,
      ElseStatements: $5,
      Keyword: "Mssql",
    }
  }

unmatched_if_statement:
  IF expression if_statement
  {
    $$ = &If{Condition: $2, IfStatements: []Statement{$3}, Keyword: "Mssql"}
  }
|
  IF expression matched_if_statement ELSE unmatched_if_statement
  {
    $$ = &If{Condition: $2, IfStatements: []Statement{$3}, ElseStatements: []Statement{$5}, Keyword: "Mssql"}
  }
|
  IF expression simple_if_body %prec NO_ELSE
  {
    $$ = &If{Condition: $2, IfStatements: $3, Keyword: "Mssql"}
  }

// A helper for any statement body that is not an unmatched IF statement
simple_if_body:
  trigger_statement_not_if
  {
    $$ = []Statement{$1}
  }
| BEGIN statement_block END
  {
    $$ = []Statement{
      &BeginEnd{
        Statements: $2,
        SuppressSemicolon: true,
      },
    }
  }

transaction_chars:
  transaction_char
  {
    $$ = SetExprs{$1}
  }
| transaction_chars ',' transaction_char
  {
    $$ = append($$, $3)
  }

transaction_char:
  ISOLATION LEVEL isolation_level
  {
    $$ = $3
  }
| READ WRITE
  {
    $$ = &SetExpr{Name: NewColIdent("tx_read_only"), Expr: NewIntVal([]byte("0"))}
  }
| READ ONLY
  {
    $$ = &SetExpr{Name: NewColIdent("tx_read_only"), Expr: NewIntVal([]byte("1"))}
  }

isolation_level:
  REPEATABLE READ
  {
    $$ = &SetExpr{Name: NewColIdent("tx_isolation"), Expr: NewStrVal([]byte("repeatable read"))}
  }
| READ COMMITTED
  {
    $$ = &SetExpr{Name: NewColIdent("tx_isolation"), Expr: NewStrVal([]byte("read committed"))}
  }
| READ UNCOMMITTED
  {
    $$ = &SetExpr{Name: NewColIdent("tx_isolation"), Expr: NewStrVal([]byte("read uncommitted"))}
  }
| SERIALIZABLE
  {
    $$ = &SetExpr{Name: NewColIdent("tx_isolation"), Expr: NewStrVal([]byte("serializable"))}
  }

sql_security:
  DEFINER
  {
    $$ = string($1)
  }
| INVOKER
  {
    $$ = string($1)
  }

set_session_or_global:
  SESSION
  {
    $$ = SessionStr
  }
| GLOBAL
  {
    $$ = GlobalStr
  }

module_arguments_opt:
  {}
| '(' module_arguments ')' {}

/* mod */
module_arguments:
  {}
| sql_id module_arguments {}
| '+' module_arguments {}
| '=' module_arguments {}
| STRING module_arguments {}
| column_definition module_arguments {}
| ',' module_arguments {}

trigger_time:
  FOR
  {
    $$ = string($1)
  }
| BEFORE
  {
    $$ = string($1)
  }
| AFTER
  {
    $$ = string($1)
  }
| INSTEAD OF
  {
    $$ = string($1) + " " + string($2)
  }

trigger_event:
  INSERT
  {
    $$ = string($1)
  }
| UPDATE
  {
    $$ = string($1)
  }
| DELETE
  {
    $$ = string($1)
  }
/* For SQLite3 */
| UPDATE OF column_list
  {
    $$ = string($1)
  }

trigger_event_list:
  trigger_event
  {
    $$ = []string{string($1)}
  }
| trigger_event_list ',' trigger_event
  {
    $$ = append($$, string($3))
  }

trigger_statements:
  trigger_statement
  {
    $$ = []Statement{$1}
  }
| trigger_statements ';' trigger_statement
  {
    $$ = append($$, $3)
  }
// For MySQL
| BEGIN trigger_statements ';' END
  {
    $$ = []Statement{
      &BeginEnd{
        Statements: $2,
      },
    }
  }
// For MSSQL
| trigger_statements trigger_statement
  {
    $$ = append($$, $2)
  }
| BEGIN trigger_statements END
  {
    $$ = []Statement{
      &BeginEnd{
        Statements: $2,
        SuppressSemicolon: true,
      },
    }
  }
| trigger_statements BEGIN trigger_statements END
  {
    $$ = append($$,
      &BeginEnd{
        Statements: $3,
        SuppressSemicolon: true,
      },
    )
  }

trigger_statement:
  trigger_statement_not_if
| if_statement

// A trigger statement that is NOT an if_statement. Used to resolve if_statement ambiguity.
trigger_statement_not_if:
  insert_statement
  {
    $$ = $1
  }
| delete_statement
| update_statement
| declare_statement
| set_statement
| cursor_statement
| while_statement
| exec_statement
| return_statement
| set_option_statement
| base_select order_by_opt limit_opt lock_opt
  {
    sel := $1.(*Select)
    sel.OrderBy = $2
    sel.Limit = $3
    sel.Lock = $4
    $$ = sel
  }

exec_statement:
  exec_keyword sql_id exec_param_list_opt
  {
    // EXEC sp_name param1, param2
    $$ = &Exec{Action: $1, Name: $2, Exprs: $3}
  }
| exec_keyword openb exec_param_list_opt closeb
  {
    // EXEC ('SELECT * FROM ...')
    $$ = &Exec{Action: $1, Exprs: $3}
  }

exec_keyword:
  EXEC    { $$ = string($1) }
| EXECUTE { $$ = string($1) }

exec_param_list_opt:
  /* empty */     { $$ = nil }
| expression_list { $$ = $1 }

return_statement:
  RETURN expression_opt
  {
    $$ = &Return{ Expr: $2 }
  }

for_each_row_opt:
  { $$ = struct{}{} }
| FOR EACH ROW
  { $$ = struct{}{} }

policy_as_opt:
  {
    $$ = nil
  }
| AS PERMISSIVE
  {
    $$ = $2
  }
| AS RESTRICTIVE
  {
    $$ = $2
  }

policy_for_opt:
  {
    $$ = nil
  }
| FOR ALL
  {
    $$ = $2
  }
| FOR SELECT
  {
    $$ = $2
  }
| FOR INSERT
  {
    $$ = $2
  }
| FOR UPDATE
  {
    $$ = $2
  }
| FOR DELETE
  {
    $$ = $2
  }

using_opt:
  {
    $$ = nil
  }
| USING expression
  {
    $$ = $2
  }

with_check_opt:
  {
    $$ = nil
  }
| WITH CHECK expression
  {
    $$ = $3
  }

unique_opt:
  {
    $$ = BoolVal(false)
  }
| UNIQUE
  {
    $$ = BoolVal(true)
  }

or_replace_opt:
  {
    $$ = nil
  }
| OR REPLACE
  {
    $$ = nil
  }

create_table_prefix:
  CREATE TABLE if_not_exists_opt table_name
  {
    $$ = &DDL{Action: CreateTable, NewName: $4}
    setDDL(yylex, $$)
  }

table_spec:
  '(' table_column_list ')' table_option_list
  {
    $$ = $2
    $$.Options = $4
  }

table_column_list:
  {
    $$ = &TableSpec{}
  }
| column_definition
  {
    $$ = &TableSpec{}
    $$.addColumn($1)
  }
| table_column_list ',' column_definition
  {
    $$.addColumn($3)
  }
| table_column_list ',' index_definition
  {
    $$.addIndex($3)
  }
| table_column_list ',' foreign_key_definition
  {
    $$.addForeignKey($3)
  }
| table_column_list ',' primary_key_definition
  {
    $$.addIndex($3)
  }
| table_column_list ',' unique_definition
  {
    $$.addIndex($3)
  }
| table_column_list ',' check_definition
  {
    $$.addCheck($3)
  }
| table_column_list ',' exclude_definition
  {
    $$ = $1
    $$.addExclusion($3)
  }

column_definition:
  reserved_sql_id column_definition_type
  {
    $$ = &ColumnDefinition{Name: $1, Type: $2}
  }
/* For SQLite3 https://www.sqlite.org/lang_keywords.html */
| STRING column_definition_type
  {
    $$ = &ColumnDefinition{Name: NewColIdent(string($1)), Type: $2}
  }
/* SQLite3 */
| ROWID column_definition_type
  {
    $$ = &ColumnDefinition{Name: NewColIdent(string($1)), Type: $2}
  }

column_type:
  numeric_type unsigned_opt zero_fill_opt
  {
    $$ = $1
    $$.Unsigned = $2
    $$.Zerofill = $3
  }
| bool_type
| char_type
| time_type
| spatial_type
// TODO: avoid reduce-reduce conflicts here
| sql_id
  {
    $$ = ColumnType{Type: $1.val}
  }
| sql_id '.' sql_id
  {
    $$ = ColumnType{Type: string($1.val) + "." + string($3.val)}
  }

column_definition_type:
  column_type array_opt
  {
    $1.NotNull = nil
    $1.Default = nil
    $1.Srid = nil
    $1.OnUpdate = nil
    $1.Autoincrement = BoolVal(false)
    $1.KeyOpt = colKeyNone
    $1.Comment = nil
    $1.Identity = nil
    $1.Array = $2
    $$ = $1
  }
| column_definition_type NULL
  {
    $1.NotNull = NewBoolVal(false)
    $$ = $1
  }
| column_definition_type NOT NULL
  {
    $1.NotNull = NewBoolVal(true)
    $$ = $1
  }
| column_definition_type default_definition
  {
    $1.Default = &DefaultDefinition{ValueOrExpression: $2}
    $$ = $1
  }
| column_definition_type CONSTRAINT sql_id default_definition
  {
    $1.Default = &DefaultDefinition{ConstraintName: $3, ValueOrExpression: $4}
    $$ = $1
  }
// for MySQL: Spatial data option
| column_definition_type srid_definition
  {
    $1.Srid = &SridDefinition{Value: $2}
    $$ = $1
  }
| column_definition_type ON UPDATE current_timestamp
  {
    $1.OnUpdate = $4
    $$ = $1
  }
| column_definition_type AUTO_INCREMENT
  {
    $1.Autoincrement = BoolVal(true)
    $$ = $1
  }
| column_definition_type AUTOINCREMENT
  {
    $1.Autoincrement = BoolVal(true)
    $$ = $1
  }
| column_definition_type PRIMARY KEY
  {
    $1.KeyOpt = colKeyPrimary
    $$ = $1
  }
| column_definition_type KEY
  {
    $1.KeyOpt = colKey
    $$ = $1
  }
| column_definition_type UNIQUE KEY
  {
    $1.KeyOpt = colKeyUniqueKey
    $$ = $1
  }
| column_definition_type UNIQUE
  {
    $1.KeyOpt = colKeyUnique
    $$ = $1
  }
| column_definition_type CHECK not_for_replication_opt openb expression closeb no_inherit_opt
  {
    $1.Check = &CheckDefinition{
      Where: *NewWhere(WhereStr, $5),
      NotForReplication: bool($3),
      NoInherit: $7,
    }
    $$ = $1
  }
| column_definition_type CONSTRAINT sql_id CHECK not_for_replication_opt openb expression closeb no_inherit_opt
  {
    $1.Check = &CheckDefinition{
      ConstraintName: $3,
      Where: *NewWhere(WhereStr, $7),
      NotForReplication: bool($5),
      NoInherit: $9,
    }
    $$ = $1
  }
| column_definition_type COMMENT_KEYWORD STRING
  {
    $1.Comment = NewStrVal($3)
    $$ = $1
  }
| column_definition_type REFERENCES table_name
  {
    $1.References = String($3)
    $$ = $1
  }
| column_definition_type REFERENCES table_name '(' column_list ')'
  {
    $1.References     = String($3)
    $1.ReferenceNames = $5
    $$ = $1
  }
// TODO: avoid a shfit/reduce conflict here
| column_definition_type REFERENCES table_name '(' column_list ')' ON DELETE reference_option
  {
    $1.References     = String($3)
    $1.ReferenceNames = $5
    $1.ReferenceOnDelete = $9
    $$ = $1
  }
| column_definition_type REFERENCES table_name '(' column_list ')' ON DELETE reference_option deferrable_opt initially_deferred_opt
  {
    $1.References              = String($3)
    $1.ReferenceNames          = $5
    $1.ReferenceOnDelete       = $9
    $1.ReferenceDeferrable     = $10
    $1.ReferenceInitiallyDeferred = $11
    $$ = $1
  }
| column_definition_type REFERENCES table_name '(' column_list ')' ON UPDATE reference_option
  {
    $1.References     = String($3)
    $1.ReferenceNames = $5
    $1.ReferenceOnUpdate = $9
    $$ = $1
  }
| column_definition_type REFERENCES table_name '(' column_list ')' ON UPDATE reference_option deferrable_opt initially_deferred_opt
  {
    $1.References              = String($3)
    $1.ReferenceNames          = $5
    $1.ReferenceOnUpdate       = $9
    $1.ReferenceDeferrable     = $10
    $1.ReferenceInitiallyDeferred = $11
    $$ = $1
  }
| column_definition_type REFERENCES table_name deferrable_opt initially_deferred_opt
  {
    $1.References              = String($3)
    $1.ReferenceDeferrable     = $4
    $1.ReferenceInitiallyDeferred = $5
    $$ = $1
  }
| column_definition_type REFERENCES table_name '(' column_list ')' deferrable_opt initially_deferred_opt
  {
    $1.References              = String($3)
    $1.ReferenceNames          = $5
    $1.ReferenceDeferrable     = $7
    $1.ReferenceInitiallyDeferred = $8
    $$ = $1
  }
| column_definition_type REFERENCES table_name '(' column_list ')' ON DELETE reference_option ON UPDATE reference_option deferrable_opt initially_deferred_opt
  {
    $1.References              = String($3)
    $1.ReferenceNames          = $5
    $1.ReferenceOnDelete       = $9
    $1.ReferenceOnUpdate       = $12
    $1.ReferenceDeferrable     = $13
    $1.ReferenceInitiallyDeferred = $14
    $$ = $1
  }
| column_definition_type REFERENCES table_name '(' column_list ')' ON UPDATE reference_option ON DELETE reference_option deferrable_opt initially_deferred_opt
  {
    $1.References              = String($3)
    $1.ReferenceNames          = $5
    $1.ReferenceOnUpdate       = $9
    $1.ReferenceOnDelete       = $12
    $1.ReferenceDeferrable     = $13
    $1.ReferenceInitiallyDeferred = $14
    $$ = $1
  }
// for MySQL and PostgreSQL
| column_definition_type AS '(' expression ')' VIRTUAL
  {
    $1.Generated = &GeneratedColumn{Expr: $4, GeneratedType: "VIRTUAL"}
    $$ = $1
  }
| column_definition_type AS '(' expression ')' STORED
  {
    $1.Generated = &GeneratedColumn{Expr: $4, GeneratedType: "STORED"}
    $$ = $1
  }
| column_definition_type GENERATED identity_behavior AS '(' expression ')' VIRTUAL
  {
    $1.Generated = &GeneratedColumn{Expr: $6, GeneratedType: "VIRTUAL"}
    $$ = $1
  }
| column_definition_type GENERATED identity_behavior AS '(' expression ')' STORED
  {
    $1.Generated = &GeneratedColumn{Expr: $6, GeneratedType: "STORED"}
    $$ = $1
  }
// for PostgreSQL
| column_definition_type GENERATED identity_behavior AS IDENTITY
  {
    $1.Identity = &IdentityOpt{Behavior: $3}
    $1.NotNull = NewBoolVal(true)
    $$ = $1
  }
| column_definition_type GENERATED identity_behavior AS IDENTITY '(' sequence_opt ')'
  {
    $1.Identity = &IdentityOpt{Behavior: $3, Sequence: $7}
    $1.NotNull = NewBoolVal(true)
    $$ = $1
  }
// for MSSQL
| column_definition_type IDENTITY '(' int_value ',' int_value ')'
  {
    $1.Identity = &IdentityOpt{Sequence: &Sequence{StartWith: NewIntVal($4), IncrementBy: NewIntVal($6)}, NotForReplication: false}
    $1.NotNull = NewBoolVal(true)
    $$ = $1
  }
// for MSSQL: IDENTITY(N,M) NOT FOR REPLICATION
| column_definition_type NOT FOR REPLICATION
  {
    $1.Identity.NotForReplication = true
    $$ = $1
  }
/* for SQLite3: Blob type */
| /* empty */
  {
    $$ = ColumnType{Type: ""}
  }

default_definition:
  DEFAULT value_expression
  {
    // Check if it's a simple value that should be stored as Value
    if val, ok := $2.(*SQLVal); ok {
      $$ = DefaultValueOrExpression{Value: val}
    } else if val, ok := $2.(BoolVal); ok {
      $$ = DefaultValueOrExpression{Value: NewBoolSQLVal(bool(val))}
    } else {
      $$ = DefaultValueOrExpression{Expr: $2}
    }
  }

srid_definition:
  SRID srid_val
  {
    $$ = $2
  }

srid_val:
  INTEGRAL
  {
    $$ = NewIntVal($1)
  }

identity_behavior:
  ALWAYS
  {
    $$ = string($1)
  }
| BY DEFAULT
  {
    $$ = string($1)+" "+string($2)
  }

sequence_opt:
  {
    $$ = &Sequence{}
  }
| sequence_opt START WITH int_value
  {
    $1.StartWith = NewIntVal($4)
    $$ = $1
  }
| sequence_opt START int_value
  {
    $1.StartWith = NewIntVal($3)
    $$ = $1
  }
| sequence_opt INCREMENT BY int_value
  {
    $1.IncrementBy = NewIntVal($4)
    $$ = $1
  }
| sequence_opt INCREMENT int_value
  {
    $1.IncrementBy = NewIntVal($3)
    $$ = $1
  }
| sequence_opt MINVALUE int_value
  {
    $1.MinValue = NewIntVal($3)
    $$ = $1
  }
| sequence_opt MAXVALUE int_value
  {
    $1.MaxValue = NewIntVal($3)
    $$ = $1
  }
| sequence_opt CACHE INTEGRAL
  {
    $1.Cache = NewIntVal($3)
    $$ = $1
  }
| sequence_opt NO MINVALUE
  {
    $1.NoMinValue = NewBoolVal(true)
    $$ = $1
  }
| sequence_opt NO MAXVALUE
  {
    $1.NoMaxValue = NewBoolVal(true)
    $$ = $1
  }
| sequence_opt NO CYCLE
  {
    $1.NoCycle = NewBoolVal(true)
    $$ = $1
  }
| sequence_opt CYCLE
  {
    $1.Cycle = NewBoolVal(true)
    $$ = $1
  }
| sequence_opt OWNED BY NONE
  {
    $1.OwnedBy = "NONE"
    $$ = $1
  }
| sequence_opt OWNED BY table_id '.' reserved_sql_id
  {
    $1.OwnedBy = string($4.v)+"."+string($6.val)
    $$ = $1
  }

current_timestamp:
  CURRENT_TIMESTAMP length_opt
  {
    $$ = NewValArgWithOpt($1, $2)
  }
| CURRENT_TIMESTAMP '(' ')'
  {
    $$ = NewValArgWithOpt($1, nil)
  }
| CURRENT_TIMESTAMP '(' INTEGRAL ')'
  {
    $$ = NewValArgWithOpt($1, NewIntVal($3))
  }
| CURRENT_TIME length_opt
  {
    $$ = NewValArgWithOpt($1, $2)
  }
| CURRENT_TIME '(' ')'
  {
    $$ = NewValArgWithOpt($1, nil)
  }
| CURRENT_DATE
  {
    $$ = NewValArgWithOpt($1, nil)
  }
| GETDATE '(' ')'
  {
    $$ = NewValArgWithOpt($1, nil)
  }

no_inherit_opt:
  {
    $$ = BoolVal(false)
  }
| NO INHERIT
  {
    $$ = BoolVal(true)
  }

numeric_type:
  int_type length_opt
  {
    $$ = $1
    $$.DisplayWidth = $2
  }
| decimal_type
  {
    $$ = $1
  }

int_type:
  BIT
  {
    $$ = ColumnType{Type: string($1)}
  }
| TINYINT
  {
    $$ = ColumnType{Type: string($1)}
  }
| SMALLINT
  {
    $$ = ColumnType{Type: string($1)}
  }
| SMALLSERIAL
  {
    $$ = ColumnType{Type: string($1)}
  }
| MEDIUMINT
  {
    $$ = ColumnType{Type: string($1)}
  }
| INT
  {
    $$ = ColumnType{Type: string($1)}
  }
| INTEGER
  {
    $$ = ColumnType{Type: string($1)}
  }
| SERIAL
  {
    $$ = ColumnType{Type: string($1)}
  }
| BIGINT
  {
    $$ = ColumnType{Type: string($1)}
  }
| BIGSERIAL
  {
    $$ = ColumnType{Type: string($1)}
  }

decimal_type:
  REAL float_length_opt
  {
    $$ = ColumnType{Type: string($1)}
    $$.Length = $2.Length
    $$.Scale = $2.Scale
  }
| DOUBLE precision_opt float_length_opt
  {
    $$ = ColumnType{Type: string($1)+$2}
    $$.Length = $3.Length
    $$.Scale = $3.Scale
  }
| FLOAT_TYPE float_length_opt
  {
    $$ = ColumnType{Type: string($1)}
    $$.Length = $2.Length
    $$.Scale = $2.Scale
  }
| DECIMAL decimal_length_opt
  {
    $$ = ColumnType{Type: string($1)}
    $$.Length = $2.Length
    $$.Scale = $2.Scale
  }
| NUMERIC decimal_length_opt
  {
    $$ = ColumnType{Type: string($1)}
    $$.Length = $2.Length
    $$.Scale = $2.Scale
  }
| MONEY
  {
    $$ = ColumnType{Type: string($1)}
  }
| SMALLMONEY
  {
    $$ = ColumnType{Type: string($1)}
  }

precision_opt:
  {
    $$ = ""
  }
| PRECISION
  {
    $$ = " " + string($1)
  }

time_type:
  DATE
  {
    $$ = ColumnType{Type: string($1)}
  }
| TIME length_opt time_zone_opt
  {
    $$ = ColumnType{Type: string($1), Length: $2, Timezone: $3}
  }
| TIMESTAMP length_opt time_zone_opt
  {
    $$ = ColumnType{Type: string($1), Length: $2, Timezone: $3}
  }
| DATETIME length_opt
  {
    $$ = ColumnType{Type: string($1), Length: $2}
  }
| DATETIME2 length_opt
  {
    $$ = ColumnType{Type: string($1), Length: $2}
  }
| DATETIMEOFFSET length_opt
  {
    $$ = ColumnType{Type: string($1), Length: $2}
  }
| SMALLDATETIME
  {
    $$ = ColumnType{Type: string($1)}
  }
| YEAR
  {
    $$ = ColumnType{Type: string($1)}
  }
| INTERVAL
  {
    $$ = ColumnType{Type: string($1)}
  }

bool_type:
  BOOL
  {
    $$ = ColumnType{Type: string($1)}
  }
| BOOLEAN
  {
    $$ = ColumnType{Type: string($1)}
  }

char_type:
  CHAR length_opt charset_opt collate_opt
  {
    $$ = ColumnType{Type: string($1), Length: $2, Charset: $3, Collate: $4}
  }
| CHARACTER varying_opt length_opt charset_opt collate_opt
  {
    $$ = ColumnType{Type: string($1)+$2, Length: $3, Charset: $4, Collate: $5}
  }
| VARCHAR max_length_opt charset_opt collate_opt
  {
    $$ = ColumnType{Type: string($1), Length: $2, Charset: $3, Collate: $4}
  }
| NCHAR length_opt charset_opt collate_opt
  {
    $$ = ColumnType{Type: string($1), Length: $2, Charset: $3, Collate: $4}
  }
| NVARCHAR max_length_opt charset_opt collate_opt
  {
    $$ = ColumnType{Type: string($1), Length: $2, Charset: $3, Collate: $4}
  }
| NTEXT
  {
    $$ = ColumnType{Type: string($1)}
  }
| BINARY length_opt
  {
    $$ = ColumnType{Type: string($1), Length: $2}
  }
| VARBINARY max_length_opt
  {
    $$ = ColumnType{Type: string($1), Length: $2}
  }
| TEXT charset_opt collate_opt
  {
    $$ = ColumnType{Type: string($1), Charset: $2, Collate: $3}
  }
| TINYTEXT charset_opt collate_opt
  {
    $$ = ColumnType{Type: string($1), Charset: $2, Collate: $3}
  }
| MEDIUMTEXT charset_opt collate_opt
  {
    $$ = ColumnType{Type: string($1), Charset: $2, Collate: $3}
  }
| LONGTEXT charset_opt collate_opt
  {
    $$ = ColumnType{Type: string($1), Charset: $2, Collate: $3}
  }
| CITEXT charset_opt collate_opt
  {
    $$ = ColumnType{Type: string($1), Charset: $2, Collate: $3}
  }
| BLOB
  {
    $$ = ColumnType{Type: string($1)}
  }
| TINYBLOB
  {
    $$ = ColumnType{Type: string($1)}
  }
| MEDIUMBLOB
  {
    $$ = ColumnType{Type: string($1)}
  }
| LONGBLOB
  {
    $$ = ColumnType{Type: string($1)}
  }
| JSON
  {
    $$ = ColumnType{Type: string($1)}
  }
| JSONB
  {
    $$ = ColumnType{Type: string($1)}
  }
| UUID
  {
    $$ = ColumnType{Type: string($1)}
  }
| ENUM '(' enum_values ')' charset_opt collate_opt
  {
    $$ = ColumnType{Type: string($1), EnumValues: $3, Charset: $5, Collate: $6}
  }
// need set_values / SetValues ?
| SET '(' enum_values ')' charset_opt collate_opt
  {
    $$ = ColumnType{Type: string($1), EnumValues: $3, Charset: $5, Collate: $6}
  }

varying_opt:
  {
    $$ = ""
  }
| VARYING
  {
    $$ = " " + string($1)
  }

spatial_type:
  GEOMETRY
  {
    $$ = ColumnType{Type: string($1)}
  }
| POINT
  {
    $$ = ColumnType{Type: string($1)}
  }
| LINESTRING
  {
    $$ = ColumnType{Type: string($1)}
  }
| POLYGON
  {
    $$ = ColumnType{Type: string($1)}
  }
| GEOMETRYCOLLECTION
  {
    $$ = ColumnType{Type: string($1)}
  }
| MULTIPOINT
  {
    $$ = ColumnType{Type: string($1)}
  }
| MULTILINESTRING
  {
    $$ = ColumnType{Type: string($1)}
  }
| MULTIPOLYGON
  {
    $$ = ColumnType{Type: string($1)}
  }
| VECTOR '(' INTEGRAL ')'
  {
    $$ = ColumnType{Type: string($1), Length: NewIntVal($3)}
  }

enum_values:
  STRING
  {
    $$ = make([]string, 0, 4)
    $$ = append($$, "'" + string($1) + "'")
  }
| enum_values ',' STRING
  {
    $$ = append($1, "'" + string($3) + "'")
  }

length_opt:
  {
    $$ = nil
  }
| '(' INTEGRAL ')'
  {
    $$ = NewIntVal($2)
  }

float_length_opt:
  {
    $$ = LengthScaleOption{}
  }
| '(' INTEGRAL ',' INTEGRAL ')'
  {
    $$ = LengthScaleOption{
      Length: NewIntVal($2),
      Scale: NewIntVal($4),
    }
  }

decimal_length_opt:
  {
    $$ = LengthScaleOption{}
  }
| '(' INTEGRAL ')'
  {
    $$ = LengthScaleOption{
      Length: NewIntVal($2),
    }
  }
| '(' INTEGRAL ',' INTEGRAL ')'
  {
    $$ = LengthScaleOption{
      Length: NewIntVal($2),
      Scale: NewIntVal($4),
    }
  }

max_length_opt:
  {
    $$ = nil
  }
| '(' INTEGRAL ')'
  {
    $$ = NewIntVal($2)
  }
| '(' ID ')'
  {
    if !strings.EqualFold(string($2), "max") {
      yylex.Error(fmt.Sprintf("syntax error around '%s'", string($2)))
    }
    $$ = NewIntVal($2)
  }


time_zone_opt:
  {
    $$ = BoolVal(false)
  }
| WITH TIME ZONE
  {
    $$ = BoolVal(true)
  }
| WITHOUT TIME ZONE
  {
    $$ = BoolVal(false)
  }

unsigned_opt:
  {
    $$ = BoolVal(false)
  }
| UNSIGNED
  {
    $$ = BoolVal(true)
  }

zero_fill_opt:
  {
    $$ = BoolVal(false)
  }
| ZEROFILL
  {
    $$ = BoolVal(true)
  }

array_opt:
  {
    $$ = BoolVal(false)
  }
| '[' ']'
  {
    $$ = BoolVal(true)
  }
| ARRAY
  {
    $$ = BoolVal(true)
  }

charset_opt:
  {
    $$ = ""
  }
| CHARACTER SET ID
  {
    $$ = string($3)
  }
| CHARACTER SET BINARY
  {
    $$ = string($3)
  }

collate_opt:
  {
    $$ = ""
  }
| BINARY
  {
    $$ = string($1) // Set pseudo collation "binary" for BINARY attribute (deprecated in future MySQL versions)
  }
| COLLATE ID
  {
    $$ = string($2)
  }

index_definition:
  index_info '(' index_column_list ')' index_option_opt index_partition_opt
  {
    $$ = &IndexDefinition{Info: $1, Columns: $3, Options: $5, Partition: $6}
  }

index_option_opt:
  {
    $$ = []*IndexOption{}
  }
| index_option_list
  {
    $$ = $1
  }
| WITH '(' mssql_index_option_list ')'
  {
    $$ = $3
  }

index_option_list:
  index_option
  {
    $$ = []*IndexOption{$1}
  }
| index_option_list index_option
  {
    $$ = append($$, $2)
  }

mssql_index_option_list:
  index_option
  {
    $$ = []*IndexOption{$1}
  }
| mssql_index_option_list ',' index_option
  {
    $$ = append($$, $3)
  }

index_option:
  USING ID
  {
    $$ = &IndexOption{Name: string($1), Value: NewStrVal($2)}
  }
| KEY_BLOCK_SIZE equal_opt INTEGRAL
  {
    // should not be string
    $$ = &IndexOption{Name: string($1), Value: NewIntVal($3)}
  }
| COMMENT_KEYWORD STRING
  {
    $$ = &IndexOption{Name: string($1), Value: NewStrVal($2)}
  }
| WITH PARSER sql_id
  {
    $$ = &IndexOption{Name: string($2), Value: NewStrVal([]byte($3.String()))}
  }
| PAD_INDEX '=' on_off
  {
    $$ = &IndexOption{Name: string($1), Value: $3}
  }
| FILLFACTOR '=' INTEGRAL
  {
    $$ = &IndexOption{Name: string($1), Value: NewIntVal($3)}
  }
| IGNORE_DUP_KEY '=' on_off
  {
    $$ = &IndexOption{Name: string($1), Value: $3}
  }
| STATISTICS_NORECOMPUTE '=' on_off
  {
    $$ = &IndexOption{Name: string($1), Value: $3}
  }
| STATISTICS_INCREMENTAL '=' on_off
  {
    $$ = &IndexOption{Name: string($1), Value: $3}
  }
| ALLOW_ROW_LOCKS '=' on_off
  {
    $$ = &IndexOption{Name: string($1), Value: $3}
  }
| ALLOW_PAGE_LOCKS '=' on_off
  {
    $$ = &IndexOption{Name: string($1), Value: $3}
  }
| DISTANCE '=' index_distance_option_value
  {
    $$ = &IndexOption{Name: string($1), Value: $3}
  }
| M '=' INTEGRAL
  {
    $$ = &IndexOption{Name: string($1), Value: NewIntVal($3)}
  }
| ID '=' vector_option_value
  {
    id := strings.Trim(strings.ToLower(string($1)), "`")
    if id != "distance" && id != "m" {
      yylex.Error(fmt.Sprintf("syntax error around '%s'", string($1)))
    }
    $$ = &IndexOption{Name: id, Value: $3}
  }


equal_opt:
  /* empty */
  {
    $$ = ""
  }
| '='
  {
    $$ = string($1)
  }

on_off:
  ON
  {
    $$ = NewBoolSQLVal(true)
  }
| OFF
  {
    $$ = NewBoolSQLVal(false)
  }

index_distance_option_value:
  EUCLIDEAN
  {
    $$ = NewStrVal($1)
  }
| COSINE
  {
    $$ = NewStrVal($1)
  }

vector_option_value:
  index_distance_option_value
  {
    $$ = $1
  }
| INTEGRAL
  {
    $$ = NewIntVal($1)
  }

// for MSSQL
index_partition_opt:
  {
    $$ = nil
  }
| ON sql_id
  {
    $$ = &IndexPartition{Name: $2.String()}
  }
| ON sql_id openb sql_id closeb
  {
    $$ = &IndexPartition{Name: $2.String(), Column: $4.String()}
  }

index_info:
  PRIMARY KEY
  {
    $$ = &IndexInfo{Type: string($1) + " " + string($2), Name: NewColIdent("PRIMARY"), Primary: true, Unique: true}
  }
| SPATIAL index_or_key ID
  {
    $$ = &IndexInfo{Type: string($1) + " " + string($2), Name: NewColIdent(string($3)), Spatial: true, Unique: false}
  }
| FULLTEXT index_or_key ID
  {
    $$ = &IndexInfo{Type: string($1) + " " + string($2), Name: NewColIdent(string($3)), Fulltext: true}
  }
| FULLTEXT ID
  {
    $$ = &IndexInfo{Type: string($1), Name: NewColIdent(string($2)), Fulltext: true}
  }
| VECTOR INDEX ID
  {
    $$ = &IndexInfo{Type: string($1) + " " + string($2), Name: NewColIdent(string($3)), Vector: true}
  }
| VECTOR INDEX
  {
    $$ = &IndexInfo{Type: string($1) + " " + string($2), Name: NewColIdent(""), Vector: true}
  }
| VECTOR KEY ID
  {
    $$ = &IndexInfo{Type: string($1) + " " + string($2), Name: NewColIdent(string($3)), Vector: true}
  }
| UNIQUE index_or_key ID
  {
    $$ = &IndexInfo{Type: string($1) + " " + string($2), Name: NewColIdent(string($3)), Unique: true}
  }
| UNIQUE ID
  {
    $$ = &IndexInfo{Type: string($1), Name: NewColIdent(string($2)), Unique: true}
  }
| UNIQUE INDEX
  {
    $$ = &IndexInfo{Type: string($1), Name: NewColIdent(""), Unique: true}
  }
| index_or_key ID clustered_opt
  {
    $$ = &IndexInfo{Type: string($1), Name: NewColIdent(string($2)), Unique: false, Clustered: $3}
  }
| index_or_key ID UNIQUE clustered_opt
  {
    $$ = &IndexInfo{Type: string($1), Name: NewColIdent(string($2)), Unique: true, Clustered: $4}
  }

index_or_key:
  INDEX
  {
    $$ = string($1)
  }
| KEY
  {
    $$ = string($1)
  }

index_column_list_or_expression:
  index_column_list
  {
    $$ = IndexColumnsOrExpression{IndexCols: $1}
  }
/* For PostgreSQL: https://www.postgresql.org/docs/14/indexes-expressional.html */
| function_call_generic
  {
    $$ = IndexColumnsOrExpression{IndexExpr: $1}
  }
| function_call_keyword
  {
    $$ = IndexColumnsOrExpression{IndexExpr: $1}
  }
| function_call_nonkeyword
  {
    $$ = IndexColumnsOrExpression{IndexExpr: $1}
  }
| function_call_conflict
  {
    $$ = IndexColumnsOrExpression{IndexExpr: $1}
  }

index_column_list:
  index_column
  {
    $$ = []IndexColumn{$1}
  }
| index_column_list ',' index_column
  {
    $$ = append($$, $3)
  }

index_column:
  sql_id length_opt asc_desc_opt
  {
    $$ = IndexColumn{Column: $1, Length: $2, Direction: $3}
  }
/* MySQL-style syntax: column_name(length) */
| sql_id '(' INTEGRAL ')' asc_desc_opt
  {
    $$ = IndexColumn{Column: $1, Length: NewIntVal($3), Direction: $5}
  }
/* For PostgreSQL */
| KEY length_opt
  {
    $$ = IndexColumn{Column: NewColIdent(string($1)), Length: $2}
  }
| sql_id operator_class
  {
    $$ = IndexColumn{Column: $1, OperatorClass: string($2)}
  }
| non_reserved_keyword length_opt asc_desc_opt
  {
    $$ = IndexColumn{Column: NewColIdent(string($1)), Length: $2, Direction: $3}
  }
| '(' expression ')' asc_desc_opt
  {
    $$ = IndexColumn{Expression: $2, Direction: $4}
  }
| function_call_generic asc_desc_opt
  {
    $$ = IndexColumn{Expression: $1, Direction: $2}
  }
| function_call_keyword asc_desc_opt
  {
    $$ = IndexColumn{Expression: $1, Direction: $2}
  }
| function_call_nonkeyword asc_desc_opt
  {
    $$ = IndexColumn{Expression: $1, Direction: $2}
  }
| function_call_conflict asc_desc_opt
  {
    $$ = IndexColumn{Expression: $1, Direction: $2}
  }

// https://www.postgresql.org/docs/9.5/brin-builtin-opclasses.html
operator_class:
  TEXT_PATTERN_OPS

foreign_key_definition:
  foreign_key_without_options not_for_replication_opt deferrable_opt initially_deferred_opt
  {
    $1.NotForReplication = bool($2)
    $1.ConstraintOptions = &ConstraintOptions{
      Deferrable: bool($3),
      InitiallyDeferred: bool($4),
    }
    $$ = $1
  }
| foreign_key_without_options ON DELETE reference_option not_for_replication_opt deferrable_opt initially_deferred_opt
  {
    $1.OnUpdate = NewColIdent("")
    $1.OnDelete = $4
    $1.NotForReplication = bool($5)
    $1.ConstraintOptions = &ConstraintOptions{
      Deferrable: bool($6),
      InitiallyDeferred: bool($7),
    }
    $$ = $1
  }
| foreign_key_without_options ON UPDATE reference_option not_for_replication_opt deferrable_opt initially_deferred_opt
  {
    $1.OnUpdate = $4
    $1.OnDelete = NewColIdent("")
    $1.NotForReplication = bool($5)
    $1.ConstraintOptions = &ConstraintOptions{
      Deferrable: bool($6),
      InitiallyDeferred: bool($7),
    }
    $$ = $1
  }
| foreign_key_without_options ON DELETE reference_option ON UPDATE reference_option not_for_replication_opt deferrable_opt initially_deferred_opt
  {
    $1.OnUpdate = $7
    $1.OnDelete = $4
    $1.NotForReplication = bool($8)
    $1.ConstraintOptions = &ConstraintOptions{
      Deferrable: bool($9),
      InitiallyDeferred: bool($10),
    }
    $$ = $1
  }
| foreign_key_without_options ON UPDATE reference_option ON DELETE reference_option not_for_replication_opt deferrable_opt initially_deferred_opt
  {
    $1.OnUpdate = $4
    $1.OnDelete = $7
    $1.NotForReplication = bool($8)
    $1.ConstraintOptions = &ConstraintOptions{
      Deferrable: bool($9),
      InitiallyDeferred: bool($10),
    }
    $$ = $1
  }

foreign_key_without_options:
  CONSTRAINT sql_id_opt FOREIGN KEY sql_id_opt '(' sql_id_list ')' REFERENCES table_name '(' sql_id_list ')'
  {
    $$ = &ForeignKeyDefinition{
      ConstraintName: $2,
      IndexName: $5,
      IndexColumns: $7,
      ReferenceName: $10,
      ReferenceColumns: $12,
    }
  }
/* For SQLite3 // SQLite Syntax: table-constraint https://www.sqlite.org/syntax/table-constraint.html */
| FOREIGN KEY sql_id_opt '(' sql_id_list ')' REFERENCES table_name '(' sql_id_list ')'
  {
    $$ = &ForeignKeyDefinition{
      IndexName: $3,
      IndexColumns: $5,
      ReferenceName: $8,
      ReferenceColumns: $10,
    }
  }

reference_option:
  RESTRICT
  {
    $$ = NewColIdent("RESTRICT")
  }
| CASCADE
  {
    $$ = NewColIdent("CASCADE")
  }
| SET NULL
  {
    $$ = NewColIdent("SET NULL")
  }
| NO ACTION
  {
    $$ = NewColIdent("NO ACTION")
  }

primary_key_definition:
  CONSTRAINT sql_id PRIMARY KEY clustered_opt '(' index_column_list ')' index_option_opt index_partition_opt
  {
    $$ = &IndexDefinition{
      Info: &IndexInfo{Type: string($3) + " " + string($4), Name: $2, Primary: true, Unique: true, Clustered: $5},
      Columns: $7,
      Options: $9,
      Partition: $10,
    }
  }
/* For SQLite3 // SQLite Syntax: table-constraint https://www.sqlite.org/syntax/table-constraint.html */
| PRIMARY KEY clustered_opt '(' index_column_list ')' index_option_opt index_partition_opt
  {
    $$ = &IndexDefinition{
      Info: &IndexInfo{Type: string($1) + " " + string($2), Primary: true, Unique: true, Clustered: $3},
      Columns: $5,
      Options: $7,
      Partition: $8,
    }
  }

unique_definition:
  CONSTRAINT sql_id UNIQUE clustered_opt '(' index_column_list ')' index_option_opt index_partition_opt deferrable_opt initially_deferred_opt
  {
    $$ = &IndexDefinition{
      Info: &IndexInfo{Type: string($3), Name: $2, Primary: false, Unique: true, Clustered: $4},
      Columns: $6,
      Options: $8,
      Partition: $9,
      ConstraintOptions: &ConstraintOptions{
        Deferrable: bool($10),
        InitiallyDeferred: bool($11),
      },
    }
  }
/* For PostgreSQL and SQLite3 */
| UNIQUE clustered_opt '(' index_column_list ')' index_option_opt index_partition_opt deferrable_opt initially_deferred_opt
  {
    $$ = &IndexDefinition{
      Info: &IndexInfo{Type: string($1), Primary: false, Unique: true, Clustered: $2},
      Columns: $4,
      Options: $6,
      Partition: $7,
      ConstraintOptions: &ConstraintOptions{
        Deferrable: bool($8),
        InitiallyDeferred: bool($9),
      },
    }
  }

check_definition:
  CONSTRAINT sql_id CHECK openb expression closeb no_inherit_opt
  {
    $$ = &CheckDefinition{
      ConstraintName: $2,
      Where: *NewWhere(WhereStr, $5),
      NoInherit: $7,
    }
  }
/* For SQLite3 // SQLite Syntax: table-options https://www.sqlite.org/syntax/table-options.html */
| CHECK openb expression closeb no_inherit_opt
  {
    $$ = &CheckDefinition{
      Where: *NewWhere(WhereStr, $3),
      NoInherit: $5,
    }
  }

exclude_definition:
  CONSTRAINT sql_id EXCLUDE openb exclude_element_list closeb
  {
    $$ = &ExclusionDefinition{
      ConstraintName: $2,
      Exclusions: $5,
    }
  }
| CONSTRAINT sql_id EXCLUDE openb exclude_element_list closeb where_expression_opt
  {
    $$ = &ExclusionDefinition{
      ConstraintName: $2,
      Exclusions: $5,
      Where: NewWhere(WhereStr, $7),
    }
  }
| CONSTRAINT sql_id EXCLUDE USING GIST openb exclude_element_list closeb
  {
    $$ = &ExclusionDefinition{
      ConstraintName: $2,
      IndexType: "GIST",
      Exclusions: $7,
    }
  }
| CONSTRAINT sql_id EXCLUDE USING GIST openb exclude_element_list closeb where_expression_opt
  {
    $$ = &ExclusionDefinition{
      ConstraintName: $2,
      IndexType: "GIST",
      Exclusions: $7,
      Where: NewWhere(WhereStr, $9),
    }
  }
| CONSTRAINT sql_id EXCLUDE USING sql_id openb exclude_element_list closeb
  {
    $$ = &ExclusionDefinition{
      ConstraintName: $2,
      IndexType: $5.String(),
      Exclusions: $7,
    }
  }
| CONSTRAINT sql_id EXCLUDE USING sql_id openb exclude_element_list closeb where_expression_opt
  {
    $$ = &ExclusionDefinition{
      ConstraintName: $2,
      IndexType: $5.String(),
      Exclusions: $7,
      Where: NewWhere(WhereStr, $9),
    }
  }

exclude_element_list:
  exclude_element
  {
    $$ = []ExclusionPair{$1}
  }
| exclude_element_list ',' exclude_element
  {
    $$ = append($1, $3)
  }

exclude_element:
  sql_id WITH '='
  {
    $$ = ExclusionPair{Column: $1, Operator: "="}
  }
| sql_id WITH sql_id
  {
    $$ = ExclusionPair{Column: $1, Operator: $3.String()}
  }
| sql_id WITH AND
  {
    $$ = ExclusionPair{Column: $1, Operator: "&&"}
  }
| expression WITH '='
  {
    // For expressions, we'll use a special column name to indicate it's an expression
    $$ = ExclusionPair{Column: NewColIdent(String($1)), Operator: "="}
  }
| expression WITH sql_id
  {
    $$ = ExclusionPair{Column: NewColIdent(String($1)), Operator: $3.String()}
  }
| expression WITH AND
  {
    $$ = ExclusionPair{Column: NewColIdent(String($1)), Operator: "&&"}
  }

/* For SQL Server */
clustered_opt:
  {
    $$ = BoolVal(false)
  }
| CLUSTERED
  {
    $$ = BoolVal(true)
  }
| NONCLUSTERED
  {
    $$ = BoolVal(false)
  }

/* For SQL Server */
unique_clustered_opt:
  {
    $$ = []BoolVal { false, false }
  }
| CLUSTERED
  {
    $$ = []BoolVal { false, true }
  }
| NONCLUSTERED
  {
    $$ = []BoolVal { false, false }
  }
| UNIQUE
  {
    $$ = []BoolVal { true, false }
  }
| UNIQUE CLUSTERED
  {
    $$ = []BoolVal { true, true }
  }
| UNIQUE NONCLUSTERED
  {
    $$ = []BoolVal { true, false }
  }

/* For SQL Server */
nonclustered_columnstore:
  COLUMNSTORE {}
| NONCLUSTERED COLUMNSTORE {}

/* For SQL Server */
not_for_replication_opt:
  {
    $$ = BoolVal(false)
  }
| NOT FOR REPLICATION
  {
    $$ = BoolVal(true)
  }

sql_id_opt:
  {
    $$ = NewColIdent("")
  }
| sql_id

sql_id_list:
  reserved_sql_id
  {
    $$ = []ColIdent{$1}
  }
| sql_id_list ',' reserved_sql_id
  {
    $$ = append($1, $3)
  }

// rather than explicitly parsing the various keywords for table options,
// just accept any number of keywords, IDs, strings, numbers, and '='
table_option_list:
  {
    $$ = map[string]string{}
  }
| table_option_list table_opt_name '=' table_opt_value
  {
    $$ = $1
    $$[string($2)] = string($4)
  }
/* For SQLite3 // SQLite Syntax: table-options https://www.sqlite.org/syntax/table-options.html */
| sqlite3_table_opt
  {
    $$ = map[string]string{}
  }
| table_option_list ',' sqlite3_table_opt
  {
    $$ = $1
  }

sqlite3_table_opt:
  WITHOUT ROWID {}
| STRICT {}

table_opt_name:
  reserved_sql_id
  {
    $$ = $1.String()
  }
| table_opt_name reserved_sql_id
  {
    $$ = $1 + " " + $2.String()
  }
| COMMENT_KEYWORD
  {
    $$ = string($1)
  }

table_opt_value:
  reserved_sql_id
  {
    $$ = $1.String()
  }
| STRING
  {
    $$ = "'" + string($1) + "'"
  }
| INTEGRAL
  {
    $$ = string($1)
  }

comment_opt:
  {
    setAllowComments(yylex, true)
  }
  comment_list
  {
    $$ = $2
    setAllowComments(yylex, false)
  }

comment_list:
  {
    $$ = nil
  }
| comment_list COMMENT
  {
    $$ = append($1, $2)
  }

union_op:
  UNION
  {
    $$ = UnionStr
  }
| UNION ALL
  {
    $$ = UnionAllStr
  }
| UNION DISTINCT
  {
    $$ = UnionDistinctStr
  }

cache_opt:
  {
    $$ = ""
  }
| SQL_NO_CACHE
  {
    $$ = SQLNoCacheStr
  }
| SQL_CACHE
  {
    $$ = SQLCacheStr
  }

distinct_opt:
  {
    $$ = ""
  }
| DISTINCT
  {
    $$ = DistinctStr
  }

straight_join_opt:
  {
    $$ = ""
  }
| STRAIGHT_JOIN
  {
    $$ = StraightJoinHint
  }

select_expression_list_opt:
  {
    $$ = nil
  }
| select_expression_list
  {
    $$ = $1
  }

select_expression_list:
  select_expression
  {
    $$ = SelectExprs{$1}
  }
| select_expression_list ',' select_expression
  {
    $$ = append($$, $3)
  }

select_expression:
  '*'
  {
    $$ = &StarExpr{}
  }
| expression as_ci_opt
  {
    $$ = &AliasedExpr{Expr: $1, As: $2}
  }
| table_id '.' '*'
  {
    $$ = &StarExpr{TableName: TableName{Name: $1}}
  }
| table_id '.' reserved_table_id '.' '*'
  {
    $$ = &StarExpr{TableName: TableName{Schema: $1, Name: $3}}
  }

as_ci_opt:
  {
    $$ = ColIdent{}
  }
| col_alias
  {
    $$ = $1
  }
| AS col_alias
  {
    $$ = $2
  }

col_alias:
  sql_id
| STRING
  {
    $$ = NewColIdent(string($1))
  }

over_expression:
  {
    $$ = nil
  }
| OVER openb closeb
  {
    $$ = &OverExpr{}
  }
| OVER openb PARTITION BY partition_by_list closeb
  {
    $$ = &OverExpr{PartitionBy: $5}
  }
| OVER openb order_by_opt closeb
  {
    $$ = &OverExpr{OrderBy: $3}
  }
| OVER openb PARTITION BY partition_by_list order_by_opt closeb
  {
    $$ = &OverExpr{PartitionBy: $5, OrderBy: $6}
  }

from_opt:
  {
    $$ = TableExprs{}
  }
| FROM table_references
  {
    $$ = $2
  }

table_references:
  table_reference
  {
    $$ = TableExprs{$1}
  }
| table_references ',' table_reference
  {
    $$ = append($$, $3)
  }

table_reference:
  table_factor
| join_table

table_factor:
  aliased_table_name
  {
    $$ = $1
  }
| subquery as_opt table_id
  {
    $$ = &AliasedTableExpr{Expr:$1, As: $3}
  }
| openb table_references closeb
  {
    $$ = &ParenTableExpr{Exprs: $2}
  }

table_hint_opt:
  {
    $$ = []string{}
  }
| WITH '(' table_hint_list ')'
  {
    $$ = $3
  }

table_hint_list:
  table_hint
  {
    $$ = []string{$1}
  }
| table_hint_list ',' table_hint
  {
    $$ = append($1, $3)
  }

table_hint:
  HOLDLOCK
  {
    $$ = string($1)
  }
| NOLOCK
  {
    $$ = string($1)
  }
| NOWAIT
  {
    $$ = string($1)
  }
| PAGLOCK
  {
    $$ = string($1)
  }
| ROWLOCK
  {
    $$ = string($1)
  }
| TABLOCK
  {
    $$ = string($1)
  }
| UPDLOCK
  {
    $$ = string($1)
  }
| READUNCOMMITTED
  {
    $$ = string($1)
  }

aliased_table_name:
  table_name as_opt_id index_hint_list table_hint_opt
  {
    $$ = &AliasedTableExpr{Expr:$1, As: $2, IndexHints: $3, TableHints: $4}
  }
| table_name PARTITION openb partition_list closeb as_opt_id index_hint_list table_hint_opt
  {
    $$ = &AliasedTableExpr{Expr:$1, Partitions: $4, As: $6, IndexHints: $7, TableHints: $8}
  }

column_list:
  sql_id
  {
    $$ = Columns{$1}
  }
/* For PostgreSQL */
| KEY
  {
    $$ = Columns{NewColIdent(string($1))}
  }
| column_list ',' sql_id
  {
    $$ = append($$, $3)
  }

partition_list:
  sql_id
  {
    $$ = Partitions{$1}
  }
| partition_list ',' sql_id
  {
    $$ = append($$, $3)
  }

// There is a grammar conflict here:
// 1: INSERT INTO a SELECT * FROM b JOIN c ON b.i = c.i
// 2: INSERT INTO a SELECT * FROM b JOIN c ON DUPLICATE KEY UPDATE a.i = 1
// When yacc encounters the ON clause, it cannot determine which way to
// resolve. The %prec override below makes the parser choose the
// first construct, which automatically makes the second construct a
// syntax error. This is the same behavior as MySQL.
join_table:
  table_reference inner_join table_factor join_condition_opt
  {
    $$ = &JoinTableExpr{LeftExpr: $1, Join: $2, RightExpr: $3, Condition: $4}
  }
| table_reference straight_join table_factor on_expression_opt
  {
    $$ = &JoinTableExpr{LeftExpr: $1, Join: $2, RightExpr: $3, Condition: $4}
  }
| table_reference outer_join table_reference join_condition
  {
    $$ = &JoinTableExpr{LeftExpr: $1, Join: $2, RightExpr: $3, Condition: $4}
  }
| table_reference natural_join table_factor
  {
    $$ = &JoinTableExpr{LeftExpr: $1, Join: $2, RightExpr: $3}
  }

join_condition:
  ON expression
  { $$ = JoinCondition{On: $2} }
| USING '(' column_list ')'
  { $$ = JoinCondition{Using: $3} }

join_condition_opt:
  %prec JOIN
  { $$ = JoinCondition{} }
| join_condition
  { $$ = $1 }

on_expression_opt:
  %prec JOIN
  { $$ = JoinCondition{} }
| ON expression
  { $$ = JoinCondition{On: $2} }

as_opt:
  { $$ = struct{}{} }
| AS
  { $$ = struct{}{} }

as_opt_id:
  {
    $$ = NewTableIdent("")
  }
| table_alias
  {
    $$ = $1
  }
| AS table_alias
  {
    $$ = $2
  }

table_alias:
  table_id
| STRING
  {
    $$ = NewTableIdent(string($1))
  }

inner_join:
  JOIN
  {
    $$ = JoinStr
  }
| INNER JOIN
  {
    $$ = JoinStr
  }
| CROSS JOIN
  {
    $$ = JoinStr
  }

straight_join:
  STRAIGHT_JOIN
  {
    $$ = StraightJoinStr
  }

outer_join:
  LEFT JOIN
  {
    $$ = LeftJoinStr
  }
| LEFT OUTER JOIN
  {
    $$ = LeftJoinStr
  }
| RIGHT JOIN
  {
    $$ = RightJoinStr
  }
| RIGHT OUTER JOIN
  {
    $$ = RightJoinStr
  }

natural_join:
  NATURAL JOIN
  {
    $$ = NaturalJoinStr
  }
| NATURAL outer_join
  {
    if $2 == LeftJoinStr {
      $$ = NaturalLeftJoinStr
    } else {
      $$ = NaturalRightJoinStr
    }
  }

into_table_name:
  INTO table_name
  {
    $$ = $2
  }
| table_name
  {
    $$ = $1
  }

table_name:
  table_id
  {
    $$ = TableName{Name: $1}
  }
| table_id '.' reserved_table_id
  {
    $$ = TableName{Schema: $1, Name: $3}
  }

index_hint_list:
  {
    $$ = nil
  }
| USE INDEX openb column_list closeb
  {
    $$ = &IndexHints{Type: UseStr, Indexes: $4}
  }
| IGNORE INDEX openb column_list closeb
  {
    $$ = &IndexHints{Type: IgnoreStr, Indexes: $4}
  }
| FORCE INDEX openb column_list closeb
  {
    $$ = &IndexHints{Type: ForceStr, Indexes: $4}
  }

where_expression_opt:
  {
    $$ = nil
  }
| WHERE expression
  {
    $$ = $2
  }

include_columns_opt:
  {
    $$ = nil
  }
| INCLUDE '(' column_list ')'
  {
    $$ = $3
  }

expression:
  condition
  {
    $$ = $1
  }
| expression AND expression
  {
    $$ = &AndExpr{Left: $1, Right: $3}
  }
| expression OR expression
  {
    $$ = &OrExpr{Left: $1, Right: $3}
  }
| NOT expression
  {
    $$ = &NotExpr{Expr: $2}
  }
| expression IS is_suffix
  {
    $$ = &IsExpr{Operator: $3, Expr: $1}
  }
| expression OUTPUT
  {
    $$ = &SuffixExpr{Expr: $1, Suffix: string($2)}
  }
| value_expression
  {
    $$ = $1
  }
| DEFAULT default_opt
  {
    $$ = &Default{ColName: $2}
  }

default_opt:
  /* empty */
  {
    $$ = ""
  }
| openb ID closeb
  {
    $$ = string($2)
  }

boolean_value:
  TRUE
  {
    $$ = BoolVal(true)
  }
| FALSE
  {
    $$ = BoolVal(false)
  }

condition:
  value_expression compare value_expression
  {
    $$ = &ComparisonExpr{Left: $1, Operator: $2, Right: $3}
  }
| value_expression compare ALL value_expression
  {
    $$ = &ComparisonExpr{Left: $1, Operator: $2, Right: $4, All: true}
  }
| value_expression compare ALL openb value_expression closeb
  {
    $$ = &ComparisonExpr{Left: $1, Operator: $2, Right: $5, All: true}
  }
| value_expression compare ANY value_expression
  {
    $$ = &ComparisonExpr{Left: $1, Operator: $2, Right: $4, Any: true}
  }
| value_expression compare ANY openb value_expression closeb
  {
    $$ = &ComparisonExpr{Left: $1, Operator: $2, Right: $5, Any: true}
  }
| value_expression compare SOME value_expression
  {
    $$ = &ComparisonExpr{Left: $1, Operator: $2, Right: $4, Any: true}
  }
| value_expression compare SOME openb value_expression closeb
  {
    $$ = &ComparisonExpr{Left: $1, Operator: $2, Right: $5, Any: true}
  }
| value_expression IN col_tuple
  {
    $$ = &ComparisonExpr{Left: $1, Operator: InStr, Right: $3}
  }
| value_expression NOT IN col_tuple
  {
    $$ = &ComparisonExpr{Left: $1, Operator: NotInStr, Right: $4}
  }
| value_expression LIKE value_expression like_escape_opt
  {
    $$ = &ComparisonExpr{Left: $1, Operator: LikeStr, Right: $3, Escape: $4}
  }
| value_expression NOT LIKE value_expression like_escape_opt
  {
    $$ = &ComparisonExpr{Left: $1, Operator: NotLikeStr, Right: $4, Escape: $5}
  }
| value_expression '!' LIKE value_expression like_escape_opt
  {
    $$ = &ComparisonExpr{Left: $1, Operator: NotLikeStr, Right: $4, Escape: $5}
  }
| value_expression REGEXP value_expression
  {
    $$ = &ComparisonExpr{Left: $1, Operator: RegexpStr, Right: $3}
  }
| value_expression NOT REGEXP value_expression
  {
    $$ = &ComparisonExpr{Left: $1, Operator: NotRegexpStr, Right: $4}
  }
| value_expression POSIX_REGEX value_expression
  {
    $$ = &ComparisonExpr{Left: $1, Operator: PosixRegexStr, Right: $3}
  }
| value_expression POSIX_REGEX_CI value_expression
  {
    $$ = &ComparisonExpr{Left: $1, Operator: PosixRegexCiStr, Right: $3}
  }
| value_expression POSIX_NOT_REGEX value_expression
  {
    $$ = &ComparisonExpr{Left: $1, Operator: PosixNotRegexStr, Right: $3}
  }
| value_expression POSIX_NOT_REGEX_CI value_expression
  {
    $$ = &ComparisonExpr{Left: $1, Operator: PosixNotRegexCiStr, Right: $3}
  }
| value_expression BETWEEN value_expression AND value_expression
  {
    $$ = &RangeCond{Left: $1, Operator: BetweenStr, From: $3, To: $5}
  }
| value_expression NOT BETWEEN value_expression AND value_expression
  {
    $$ = &RangeCond{Left: $1, Operator: NotBetweenStr, From: $4, To: $6}
  }
| EXISTS subquery
  {
    $$ = &ExistsExpr{Subquery: $2}
  }
/* For MSSQL */
| UPDATE openb column_name closeb
  {
    $$ = &UpdateFuncExpr{Name: $3}
  }
| COLUMNS_UPDATED openb closeb
  {
    $$ = &UpdateFuncExpr{Name: nil}
  }
| openb condition closeb
  {
    $$ = &ParenExpr{Expr: $2}
  }

is_suffix:
  NULL
  {
    $$ = IsNullStr
  }
| NOT NULL
  {
    $$ = IsNotNullStr
  }
| TRUE
  {
    $$ = IsTrueStr
  }
| NOT TRUE
  {
    $$ = IsNotTrueStr
  }
| FALSE
  {
    $$ = IsFalseStr
  }
| NOT FALSE
  {
    $$ = IsNotFalseStr
  }

compare:
  '='
  {
    $$ = EqualStr
  }
| '<'
  {
    $$ = LessThanStr
  }
| '>'
  {
    $$ = GreaterThanStr
  }
| LE
  {
    $$ = LessEqualStr
  }
| GE
  {
    $$ = GreaterEqualStr
  }
| NE
  {
    $$ = NotEqualStr
  }
| NULL_SAFE_EQUAL
  {
    $$ = NullSafeEqualStr
  }
| POSIX_REGEX
  {
    $$ = PosixRegexStr
  }
| POSIX_REGEX_CI
  {
    $$ = PosixRegexCiStr
  }
| POSIX_NOT_REGEX
  {
    $$ = PosixNotRegexStr
  }
| POSIX_NOT_REGEX_CI
  {
    $$ = PosixNotRegexCiStr
  }

like_escape_opt:
  {
    $$ = nil
  }
| ESCAPE value_expression
  {
    $$ = $2
  }

col_tuple:
  row_tuple
  {
    $$ = $1
  }
| subquery
  {
    $$ = $1
  }
| LIST_ARG
  {
    $$ = ListArg($1)
  }

subquery:
  openb select_statement closeb
  {
    $$ = &Subquery{$2}
  }

expression_list:
  expression
  {
    $$ = Exprs{$1}
  }
| expression_list ',' expression
  {
    $$ = append($1, $3)
  }

value_expression:
  value
  {
    $$ = $1
  }
| boolean_value
  {
    $$ = $1
  }
| DATE STRING
  {
    // PostgreSQL date literal syntax: DATE '2022-01-01'
    // This is syntactic sugar for '2022-01-01', so just use the string value
    $$ = NewStrVal($2)
  }
| column_name
  {
    $$ = $1
  }
| new_qualifier_column_name
  {
    $$ = $1
  }
| tuple_expression
  {
    $$ = $1
  }
| array_constructor
  {
    $$ = $1
  }
| subquery
  {
    $$ = $1
  }
| value_expression '&' value_expression
  {
    $$ = &BinaryExpr{Left: $1, Operator: BitAndStr, Right: $3}
  }
| value_expression '|' value_expression
  {
    $$ = &BinaryExpr{Left: $1, Operator: BitOrStr, Right: $3}
  }
| value_expression '^' value_expression
  {
    $$ = &BinaryExpr{Left: $1, Operator: BitXorStr, Right: $3}
  }
| value_expression '+' value_expression
  {
    $$ = &BinaryExpr{Left: $1, Operator: PlusStr, Right: $3}
  }
| value_expression '-' value_expression
  {
    $$ = &BinaryExpr{Left: $1, Operator: MinusStr, Right: $3}
  }
| value_expression '*' value_expression
  {
    $$ = &BinaryExpr{Left: $1, Operator: MultStr, Right: $3}
  }
| value_expression '/' value_expression
  {
    $$ = &BinaryExpr{Left: $1, Operator: DivStr, Right: $3}
  }
| value_expression DIV value_expression
  {
    $$ = &BinaryExpr{Left: $1, Operator: IntDivStr, Right: $3}
  }
| value_expression '%' value_expression
  {
    $$ = &BinaryExpr{Left: $1, Operator: ModStr, Right: $3}
  }
| value_expression MOD value_expression
  {
    $$ = &BinaryExpr{Left: $1, Operator: ModStr, Right: $3}
  }
| value_expression SHIFT_LEFT value_expression
  {
    $$ = &BinaryExpr{Left: $1, Operator: ShiftLeftStr, Right: $3}
  }
| value_expression SHIFT_RIGHT value_expression
  {
    $$ = &BinaryExpr{Left: $1, Operator: ShiftRightStr, Right: $3}
  }
| column_name JSON_EXTRACT_OP value
  {
    $$ = &BinaryExpr{Left: $1, Operator: JSONExtractOp, Right: $3}
  }
| column_name JSON_UNQUOTE_EXTRACT_OP value
  {
    $$ = &BinaryExpr{Left: $1, Operator: JSONUnquoteExtractOp, Right: $3}
  }
| value_expression TYPECAST numeric_type
  {
    colType := $3
    $$ = &CastExpr{Expr: $1, Type: &colType}
  }
| value_expression COLLATE charset
  {
    $$ = &CollateExpr{Expr: $1, Charset: $3}
  }
| value_expression TYPECAST TIMESTAMP WITH TIME ZONE
  {
    timestampType := ColumnType{Type: "timestamp", Timezone: BoolVal(true)}
    $$ = &CastExpr{Expr: $1, Type: &timestampType}
  }
| BINARY value_expression %prec UNARY
  {
    $$ = &UnaryExpr{Operator: BinaryStr, Expr: $2}
  }
| UNDERSCORE_BINARY value_expression %prec UNARY
  {
    $$ = &UnaryExpr{Operator: UBinaryStr, Expr: $2}
  }
| '+'  value_expression %prec UNARY
  {
    if num, ok := $2.(*SQLVal); ok && num.Type == IntVal {
      $$ = num
    } else {
      $$ = &UnaryExpr{Operator: UPlusStr, Expr: $2}
    }
  }
| '-'  value_expression %prec UNARY
  {
    if num, ok := $2.(*SQLVal); ok && num.Type == IntVal {
      // Handle double negative
      if num.Val[0] == '-' {
        num.Val = num.Val[1:]
        $$ = num
      } else {
        $$ = NewIntVal(append([]byte("-"), num.Val...))
      }
    } else {
      $$ = &UnaryExpr{Operator: UMinusStr, Expr: $2}
    }
  }
| '~'  value_expression
  {
    $$ = &UnaryExpr{Operator: TildaStr, Expr: $2}
  }
| '!' value_expression %prec UNARY
  {
    $$ = &UnaryExpr{Operator: BangStr, Expr: $2}
  }
| INTERVAL value_expression
  {
    // This rule prevents the usage of INTERVAL
    // as a function. If support is needed for that,
    // we'll need to revisit this. The solution
    // will be non-trivial because of grammar conflicts.
    $$ = &IntervalExpr{Expr: $2}
  }
| INTERVAL value_expression sql_id
  {
    // This rule prevents the usage of INTERVAL
    // as a function. If support is needed for that,
    // we'll need to revisit this. The solution
    // will be non-trivial because of grammar conflicts.
    $$ = &IntervalExpr{Expr: $2, Unit: $3.String()}
  }
| value_expression TYPECAST simple_convert_type
  {
    // Convert ConvertType to ColumnType
    convertType := $3
    colType := ColumnType{
      Type: convertType.Type,
      Length: convertType.Length,
      Scale: convertType.Scale,
      Charset: convertType.Charset,
    }
    $$ = &CastExpr{Expr: $1, Type: &colType}
  }
| function_call_generic
| function_call_keyword
| function_call_nonkeyword
| function_call_conflict
| VARIADIC array_constructor
  {
    $$ = $2
  }
| CURRENT_USER
  {
    $$ = &ColName{Name: NewColIdent(string($1))}
  }
| openb value_expression closeb
  {
    $$ = &ParenExpr{Expr: $2}
  }

/*
 * Regular function calls without special token or syntax, guaranteed to not
 * introduce side effects due to being a simple identifier
 */
function_call_generic:
  sql_id openb select_expression_list_opt closeb
  {
    $$ = &FuncExpr{Name: $1, Exprs: $3}
  }
| sql_id openb DISTINCT select_expression_list closeb
  {
    $$ = &FuncExpr{Name: $1, Distinct: true, Exprs: $4}
  }
| sql_id openb select_expression_list closeb over_expression
  {
    $$ = &FuncExpr{Name: $1, Exprs: $3, Over: $5}
  }
| LAG openb select_expression_list closeb over_expression
  {
    $$ = &FuncExpr{Name: NewColIdent(string($1)), Exprs: $3, Over: $5}
  }
| LEAD openb select_expression_list closeb over_expression
  {
    $$ = &FuncExpr{Name: NewColIdent(string($1)), Exprs: $3, Over: $5}
  }
| table_id '.' reserved_sql_id openb select_expression_list_opt closeb
  {
    $$ = &FuncExpr{Qualifier: $1, Name: $3, Exprs: $5}
  }
| sql_id openb expression_list closeb
  {
    $$ = &FuncCallExpr{Name: $1, Exprs: $3}
  }

/*
 * Function calls using reserved keywords, with dedicated grammar rules
 * as a result
 */
function_call_keyword:
  LEFT openb select_expression_list closeb
  {
    $$ = &FuncExpr{Name: NewColIdent("left"), Exprs: $3}
  }
| RIGHT openb select_expression_list closeb
  {
    $$ = &FuncExpr{Name: NewColIdent("right"), Exprs: $3}
  }
| CONVERT openb expression ',' convert_type closeb
  {
    $$ = &ConvertExpr{Expr: $3, Type: $5}
  }
// for MSSQL
| CONVERT openb convert_type ',' expression closeb
  {
    $$ = &ConvertExpr{Action: Type1stStr, Type: $3, Expr: $5}
  }
| CONVERT openb convert_type ',' expression ',' value_expression closeb
  {
    $$ = &ConvertExpr{Action: Type1stStr, Type: $3, Expr: $5, Style: $7}
   }
| CAST openb expression AS convert_type closeb
  {
    $$ = &ConvertExpr{Action: CastStr, Expr: $3, Type: $5}
  }
| COALESCE openb select_expression_list closeb
  {
    $$ = &FuncExpr{Name: NewColIdent("coalesce"), Exprs: $3}
  }
| CONVERT openb expression USING charset closeb
  {
    $$ = &ConvertUsingExpr{Expr: $3, Type: $5}
  }
| SUBSTR openb select_expression ',' value_expression closeb
  {
    $$ = &SubstrExpr{Name: $3, From: $5, To: nil}
  }
| SUBSTR openb select_expression ',' value_expression ',' value_expression closeb
  {
    $$ = &SubstrExpr{Name: $3, From: $5, To: $7}
  }
| SUBSTR openb select_expression FROM value_expression closeb
  {
    $$ = &SubstrExpr{Name: $3, From: $5, To: nil}
  }
| SUBSTR openb select_expression FROM value_expression FOR value_expression closeb
  {
    $$ = &SubstrExpr{Name: $3, From: $5, To: $7}
  }
| SUBSTRING openb select_expression ',' value_expression closeb
  {
    $$ = &SubstrExpr{Name: $3, From: $5, To: nil}
  }
| SUBSTRING openb select_expression ',' value_expression ',' value_expression closeb
  {
    $$ = &SubstrExpr{Name: $3, From: $5, To: $7}
  }
| SUBSTRING openb select_expression FROM value_expression closeb
  {
    $$ = &SubstrExpr{Name: $3, From: $5, To: nil}
  }
| SUBSTRING openb select_expression FROM value_expression FOR value_expression closeb
  {
    $$ = &SubstrExpr{Name: $3, From: $5, To: $7}
  }
| MATCH openb select_expression_list closeb AGAINST openb value_expression match_option closeb
  {
    $$ = &MatchExpr{Columns: $3, Expr: $7, Option: $8}
  }
| GROUP_CONCAT openb distinct_opt select_expression_list order_by_opt separator_opt closeb
  {
    $$ = &GroupConcatExpr{Distinct: $3, Exprs: $4, OrderBy: $5, Separator: $6}
  }
| CASE expression_opt when_expression_list else_expression_opt END
  {
    $$ = &CaseExpr{Expr: $2, Whens: $3, Else: $4}
  }
| VALUES openb column_name closeb
  {
    $$ = &ValuesFuncExpr{Name: $3}
  }
/* SQL Server */
| NEXT VALUE FOR table_id
  {
    $$ = &NextSeqValExpr{SequenceName: $4}
  }
| UUID openb closeb
  {
    $$ = &FuncExpr{Name: NewColIdent(string($1))}
  }
| NOW openb closeb
  {
    $$ = &FuncExpr{Name: NewColIdent(string($1))}
  }
| GETDATE openb closeb
  {
    $$ = &FuncExpr{Name: NewColIdent(string($1))}
  }

/*
 * Function calls using non reserved keywords but with special syntax forms.
 * Dedicated grammar rules are needed because of the special syntax
 */
function_call_nonkeyword:
// for MSSQL
  CURRENT_TIMESTAMP
  {
    $$ = &ColName{Name: NewColIdent(string($1))}
  }
| CURRENT_TIMESTAMP openb closeb
  {
    $$ = &FuncExpr{Name:NewColIdent("current_timestamp")}
  }
| CURRENT_TIMESTAMP openb INTEGRAL closeb
  {
    $$ = &FuncExpr{
      Name: NewColIdent("current_timestamp"),
      Exprs: SelectExprs{&AliasedExpr{Expr: NewIntVal($3)}},
    }
  }
| UTC_TIMESTAMP func_datetime_precision_opt
  {
    $$ = &FuncExpr{Name:NewColIdent("utc_timestamp")}
  }
| UTC_TIME func_datetime_precision_opt
  {
    $$ = &FuncExpr{Name:NewColIdent("utc_time")}
  }
| UTC_DATE func_datetime_precision_opt
  {
    $$ = &FuncExpr{Name:NewColIdent("utc_date")}
  }
// now
| LOCALTIME func_datetime_precision_opt
  {
    $$ = &FuncExpr{Name:NewColIdent("localtime")}
  }
// now
| LOCALTIMESTAMP func_datetime_precision_opt
  {
    $$ = &FuncExpr{Name:NewColIdent("localtimestamp")}
  }
// curdate
| CURRENT_DATE func_datetime_precision_opt
  {
    $$ = &FuncExpr{Name:NewColIdent("current_date")}
  }
// curtime
| CURRENT_TIME func_datetime_precision_opt
  {
    $$ = &FuncExpr{Name:NewColIdent("current_time")}
  }
| TYPECAST simple_convert_type
  {
    $$ = &ConvertExpr{Type: $2}
  }

func_datetime_precision_opt:
/* empty */
| openb closeb

/*
 * Function calls using non reserved keywords with *normal* syntax forms. Because
 * the names are non-reserved, they need a dedicated rule so as not to conflict
 */
function_call_conflict:
  IF openb select_expression_list closeb
  {
    $$ = &FuncExpr{Name: NewColIdent("if"), Exprs: $3}
  }
| DATABASE openb select_expression_list_opt closeb
  {
    $$ = &FuncExpr{Name: NewColIdent("database"), Exprs: $3}
  }
| MOD openb select_expression_list closeb
  {
    $$ = &FuncExpr{Name: NewColIdent("mod"), Exprs: $3}
  }
| REPLACE openb select_expression_list closeb
  {
    $$ = &FuncExpr{Name: NewColIdent("replace"), Exprs: $3}
  }

match_option:
  /* empty */
  {
    $$ = ""
  }
| IN BOOLEAN MODE
  {
    $$ = BooleanModeStr
  }
| IN NATURAL LANGUAGE MODE
  {
    $$ = NaturalLanguageModeStr
  }
| IN NATURAL LANGUAGE MODE WITH QUERY EXPANSION
  {
    $$ = NaturalLanguageModeWithQueryExpansionStr
  }
| WITH QUERY EXPANSION
  {
    $$ = QueryExpansionStr
  }

charset:
  ID
  {
    $$ = string($1)
  }
| STRING
  {
    $$ = string($1)
  }

convert_type:
  BINARY length_opt
  {
    $$ = &ConvertType{Type: string($1), Length: $2}
  }
| CHAR length_opt charset_opt
  {
    $$ = &ConvertType{Type: string($1), Length: $2, Charset: $3, Operator: CharacterSetStr}
  }
| CHAR length_opt ID
  {
    $$ = &ConvertType{Type: string($1), Length: $2, Charset: string($3)}
  }
| DATE
  {
    $$ = &ConvertType{Type: string($1)}
  }
| DATETIME length_opt
  {
    $$ = &ConvertType{Type: string($1), Length: $2}
  }
| DECIMAL decimal_length_opt
  {
    $$ = &ConvertType{Type: string($1)}
    $$.Length = $2.Length
    $$.Scale = $2.Scale
  }
| JSON
  {
    $$ = &ConvertType{Type: string($1)}
  }
| JSONB
  {
    $$ = &ConvertType{Type: string($1)}
  }
| SIGNED
  {
    $$ = &ConvertType{Type: string($1)}
  }
| SIGNED INTEGER
  {
    $$ = &ConvertType{Type: string($1)}
  }
| TIME length_opt
  {
    $$ = &ConvertType{Type: string($1), Length: $2}
  }
| UNSIGNED
  {
    $$ = &ConvertType{Type: string($1)}
  }
| UNSIGNED INTEGER
  {
    $$ = &ConvertType{Type: string($1)}
  }
| BIGINT
  {
    $$ = &ConvertType{Type: string($1)}
  }
| BIT
  {
    $$ = &ConvertType{Type: string($1)}
  }
| INT
  {
    $$ = &ConvertType{Type: string($1)}
  }
| MONEY
  {
    $$ = &ConvertType{Type: string($1)}
  }
| NUMERIC decimal_length_opt
  {
    $$ = &ConvertType{Type: string($1), Length: $2.Length, Scale: $2.Scale}
  }
| SMALLINT
  {
    $$ = &ConvertType{Type: string($1)}
  }
| SMALLMONEY
  {
    $$ = &ConvertType{Type: string($1)}
  }
| TINYINT
  {
    $$ = &ConvertType{Type: string($1)}
  }
| FLOAT_TYPE
  {
    $$ = &ConvertType{Type: string($1)}
  }
| REAL
  {
    $$ = &ConvertType{Type: string($1)}
  }
| DATETIME2 length_opt
  {
    $$ = &ConvertType{Type: string($1), Length: $2}
  }
| DATETIMEOFFSET length_opt
  {
    $$ = &ConvertType{Type: string($1), Length: $2}
  }
| SMALLDATETIME
  {
    $$ = &ConvertType{Type: string($1)}
  }
| TEXT
  {
    $$ = &ConvertType{Type: string($1)}
  }
| VARCHAR length_opt
  {
    $$ = &ConvertType{Type: string($1), Length: $2}
  }
| NCHAR length_opt
  {
    $$ = &ConvertType{Type: string($1), Length: $2}
  }
| NTEXT
  {
    $$ = &ConvertType{Type: string($1)}
  }
| NVARCHAR length_opt
  {
    $$ = &ConvertType{Type: string($1), Length: $2}
  }
| VARBINARY length_opt
  {
    $$ = &ConvertType{Type: string($1), Length: $2}
  }

simple_convert_type:
  BINARY
  {
    $$ = &ConvertType{Type: string($1)}
  }
| CHARACTER VARYING
  {
    $$ = &ConvertType{Type: string($1)+" "+string($2)}
  }
| DATE
  {
    $$ = &ConvertType{Type: string($1)}
  }
| DATETIME
  {
    $$ = &ConvertType{Type: string($1)}
  }
| int_type
  {
    $$ = &ConvertType{Type: $1.Type}
  }
| bool_type
  {
    $$ = &ConvertType{Type: $1.Type}
  }
| TEXT
  {
    $$ = &ConvertType{Type: string($1)}
  }
| UUID
  {
    $$ = &ConvertType{Type: string($1)}
  }
| int_type '[' ']'
  {
    $$ = &ConvertType{Type: $1.Type + "[]"}
  }
| TEXT '[' ']'
  {
    $$ = &ConvertType{Type: string($1) + "[]"}
  }
| INTERVAL
  {
    $$ = &ConvertType{Type: string($1)}
  }
| BPCHAR
  {
    $$ = &ConvertType{Type: string($1)}
  }
| JSON
  {
    $$ = &ConvertType{Type: string($1)}
  }
| JSONB
  {
    $$ = &ConvertType{Type: string($1)}
  }
| TIMESTAMP
  {
    $$ = &ConvertType{Type: string($1)}
  }
| TIMESTAMP WITH TIME ZONE
  {
    $$ = &ConvertType{Type: string($1)+" with time zone"}
  }
| TIMESTAMP WITHOUT TIME ZONE
  {
    $$ = &ConvertType{Type: string($1)+" without time zone"}
  }
| sql_id
  {
    $$ = &ConvertType{Type: $1.val}
  }
| sql_id '.' sql_id
  {
    $$ = &ConvertType{Type: string($1.val) + "." + string($3.val)}
  }

expression_opt:
  {
    $$ = nil
  }
| expression
  {
    $$ = $1
  }

separator_opt:
  {
    $$ = string("")
  }
| SEPARATOR STRING
  {
    $$ = " separator '"+string($2)+"'"
  }

when_expression_list:
  when_expression
  {
    $$ = []*When{$1}
  }
| when_expression_list when_expression
  {
    $$ = append($1, $2)
  }

when_expression:
  WHEN expression THEN expression
  {
    $$ = &When{Cond: $2, Val: $4}
  }

when_expression_opt:
  { $$ = struct{}{} }
| WHEN expression
  { $$ = struct{}{} }

else_expression_opt:
  {
    $$ = nil
  }
| ELSE expression
  {
    $$ = $2
  }

column_name:
  sql_id
  {
    $$ = &ColName{Name: $1}
  }
| non_reserved_keyword
  {
    $$ = &ColName{Name: NewColIdent(string($1))}
  }
| table_id '.' reserved_sql_id
  {
    $$ = &ColName{Qualifier: TableName{Name: $1}, Name: $3}
  }
| table_id '.' reserved_table_id '.' reserved_sql_id
  {
    $$ = &ColName{Qualifier: TableName{Schema: $1, Name: $3}, Name: $5}
  }

new_qualifier_column_name:
  NEW '.' reserved_sql_id
  {
    $$ = &NewQualifierColName{Name: $3}
  }

value:
  STRING
  {
    $$ = NewStrVal($1)
  }
| UNICODE_STRING
  {
    $$ = NewUnicodeStrVal($1)
  }
// For MySQL
| sql_id STRING
  {
    // Ignoring _charset_name as a workaround
    $$ = NewStrVal($2)
  }
| HEX
  {
    $$ = NewHexVal($1)
  }
| BIT_LITERAL
  {
    $$ = NewBitVal($1)
  }
| INTEGRAL
  {
    $$ = NewIntVal($1)
  }
| FLOAT
  {
    $$ = NewFloatVal($1)
  }
| HEXNUM
  {
    $$ = NewHexNum($1)
  }
| VALUE_ARG
  {
    $$ = NewValArg($1)
  }
| NULL
  {
    $$ = &NullVal{}
  }

int_value:
  INTEGRAL
  {
    $$ = $1
  }
| '-' INTEGRAL
  {
    $$ = append([]byte("-"), $2...)
  }

group_by_opt:
  {
    $$ = nil
  }
| GROUP BY expression_list
  {
    $$ = $3
  }

having_opt:
  {
    $$ = nil
  }
| HAVING expression
  {
    $$ = $2
  }

partition_by_list:
  partition
  {
    $$ = PartitionBy{$1}
  }
| partition_by_list ',' partition
  {
    $$ = append($1, $3)
  }

partition:
  expression
  {
    $$ = &Partition{Expr: $1}
  }

order_by_opt:
  {
    $$ = nil
  }
| ORDER BY order_list
  {
    $$ = $3
  }

order_list:
  order
  {
    $$ = OrderBy{$1}
  }
| order_list ',' order
  {
    $$ = append($1, $3)
  }

order:
  expression asc_desc_opt
  {
    $$ = &Order{Expr: $1, Direction: $2}
  }

asc_desc_opt:
  {
    $$ = AscScr
  }
| ASC
  {
    $$ = AscScr
  }
| DESC
  {
    $$ = DescScr
  }

limit_opt:
  {
    $$ = nil
  }
| LIMIT expression
  {
    $$ = &Limit{Rowcount: $2}
  }
| LIMIT expression ',' expression
  {
    $$ = &Limit{Offset: $2, Rowcount: $4}
  }
| LIMIT expression OFFSET expression
  {
    $$ = &Limit{Offset: $4, Rowcount: $2}
  }

lock_opt:
  {
    $$ = ""
  }
| FOR UPDATE
  {
    $$ = ForUpdateStr
  }
| LOCK IN SHARE MODE
  {
    $$ = ShareModeStr
  }

// insert_data expands all combinations into a single rule.
// This avoids a shift/reduce conflict while encountering the
// following two possible constructs:
// insert into t1(a, b) (select * from t2)
// insert into t1(select * from t2)
// Because the rules are together, the parser can keep shifting
// the tokens until it disambiguates a as sql_id and select as keyword.
insert_data:
  VALUES tuple_list
  {
    $$ = &Insert{Rows: $2}
  }
| select_statement
  {
    $$ = &Insert{Rows: $1}
  }
| openb select_statement closeb
  {
    // Drop the redundant parenthesis.
    $$ = &Insert{Rows: $2}
  }
| openb ins_column_list closeb VALUES tuple_list
  {
    $$ = &Insert{Columns: $2, Rows: $5}
  }
| openb ins_column_list closeb select_statement
  {
    $$ = &Insert{Columns: $2, Rows: $4}
  }
| openb ins_column_list closeb openb select_statement closeb
  {
    // Drop the redundant parenthesis.
    $$ = &Insert{Columns: $2, Rows: $5}
  }

ins_column_list:
  ins_column
  {
    $$ = Columns{$1}
  }
| ins_column_list ',' ins_column
  {
    $$ = append($1, $3)
  }

ins_column:
  sql_id
  {
    $$ = $1
  }
| sql_id '.' sql_id
  {
    $$ = $3
  }
| reserved_keyword
  {
    $$ = NewColIdent(string($1))
  }
| non_reserved_keyword
  {
    $$ = NewColIdent(string($1))
  }

on_dup_opt:
  {
    $$ = nil
  }
| ON DUPLICATE KEY UPDATE update_list
  {
    $$ = $5
  }

tuple_list:
  tuple_or_empty
  {
    $$ = Values{$1}
  }
| tuple_list ',' tuple_or_empty
  {
    $$ = append($1, $3)
  }

tuple_or_empty:
  row_tuple
  {
    $$ = $1
  }
| openb closeb
  {
    $$ = ValTuple{}
  }

row_tuple:
  openb expression_list closeb
  {
    $$ = ValTuple($2)
  }

tuple_expression:
  row_tuple
  {
    if len($1) == 1 {
      $$ = &ParenExpr{$1[0]}
    } else {
      $$ = $1
    }
  }

update_list:
  update_expression
  {
    $$ = UpdateExprs{$1}
  }
| update_list ',' update_expression
  {
    $$ = append($1, $3)
  }

update_expression:
  column_name '=' expression
  {
    $$ = &UpdateExpr{Name: $1, Expr: $3}
  }

set_list:
  set_expression
  {
    $$ = SetExprs{$1}
  }
| set_list ',' set_expression
  {
    $$ = append($1, $3)
  }

set_expression:
  reserved_sql_id '=' ON
  {
    $$ = &SetExpr{Name: $1, Expr: NewStrVal([]byte("on"))}
  }
| reserved_sql_id '=' OFF
  {
    $$ = &SetExpr{Name: $1, Expr: NewStrVal([]byte("off"))}
  }
| reserved_sql_id '=' expression
  {
    $$ = &SetExpr{Name: $1, Expr: $3}
  }
// MySQL extension of triggers
| NEW '.' reserved_sql_id '=' expression
  {
    $$ = &SetExpr{Name: NewColIdent("NEW." + $3.val), Expr: $5}
  }
| charset_or_character_set charset_value collate_opt
  {
    $$ = &SetExpr{Name: NewColIdent(string($1)), Expr: $2}
  }

set_option_statement:
  set_bool_option_statement
  {
    $$ = $1
  }

set_bool_option_statement:
  SET bool_option_name_list on_off
  {
    $$ = &SetBoolOption{OptionNames: $2, Value: $3}
  }

charset_or_character_set:
  CHARSET
| CHARACTER SET
  {
    $$ = []byte("charset")
  }
| NAMES

charset_value:
  sql_id
  {
    $$ = NewStrVal([]byte($1.String()))
  }
| STRING
  {
    $$ = NewStrVal($1)
  }
| DEFAULT
  {
    $$ = &Default{}
  }

if_not_exists_opt:
  { $$ = struct{}{} }
| IF NOT EXISTS
  { $$ = struct{}{} }

ignore_opt:
  { $$ = "" }
| IGNORE
  { $$ = IgnoreStr }

sql_id:
  ID
  {
    $$ = NewColIdent(string($1))
  }
| LEVEL
  {
    $$ = NewColIdent(string($1))
  }

reserved_sql_id:
  sql_id
| CHARSET
  {
    $$ = NewColIdent(string($1))
  }
| KEY_BLOCK_SIZE
  {
    $$ = NewColIdent(string($1))
  }
| reserved_keyword
  {
    $$ = NewColIdent(string($1))
  }
| non_reserved_keyword
  {
    $$ = NewColIdent(string($1))
  }

table_id:
  ID
  {
    $$ = NewTableIdent(string($1))
  }
| non_reserved_keyword
  {
    $$ = NewTableIdent(string($1))
  }
/* For SQLite3 https://www.sqlite.org/lang_keywords.html */
| STRING
  {
    $$ = NewTableIdent(string($1))
  }

reserved_table_id:
  table_id
| reserved_keyword
  {
    $$ = NewTableIdent(string($1))
  }

deferrable_opt:
  /* empty */
  {
    $$ = BoolVal(false)
  }
| DEFERRABLE
  {
    $$ = BoolVal(true)
  }
| NOT DEFERRABLE
  {
    $$ = BoolVal(false)
  }

initially_deferred_opt:
  /* empty */
  {
    $$ = BoolVal(false)
  }
| INITIALLY DEFERRED
  {
    $$ = BoolVal(true)
  }
| INITIALLY IMMEDIATE
  {
    $$ = BoolVal(false)
  }

/* For PostgreSQL. https://www.postgresql.org/docs/14/sql-expressions.html#SQL-SYNTAX-ARRAY-CONSTRUCTORS */
array_constructor:
  ARRAY '[' array_element_list ']'
  {
    $$ = &ArrayConstructor{Elements: $3}
  }
| ARRAY '[' ']'
  {
    $$ = &ArrayConstructor{Elements: nil}
  }

/* For PostgreSQL */
array_element_list:
  array_element
  {
    $$ = ArrayElements{$1}
  }
| array_element_list ',' array_element
  {
    $$ = append($$, $3)
  }

/* For PostgreSQL */
array_element:
  STRING
  {
    $$ = NewStrVal($1)
  }
| INTEGRAL
  {
    $$ = NewIntVal($1)
  }
| FLOAT
  {
    $$ = NewFloatVal($1)
  }
| HEXNUM
  {
    $$ = NewHexNum($1)
  }
| VALUE_ARG
  {
    $$ = NewValArg($1)
  }
| NULL
  {
    $$ = &NullVal{}
  }
| TRUE
  {
    $$ = BoolVal(true)
  }
| FALSE
  {
    $$ = BoolVal(false)
  }
| value_expression TYPECAST simple_convert_type
  {
    // Convert ConvertType to ColumnType
    convertType := $3
    colType := ColumnType{
      Type: convertType.Type,
      Length: convertType.Length,
      Scale: convertType.Scale,
      Charset: convertType.Charset,
    }
    $$ = &CastExpr{Expr: $1, Type: &colType}
  }

bool_option_name_list:
  bool_option_name
  {
    $$ = []string{string($1)}
  }
| bool_option_name_list ',' bool_option_name
  {
    $$ = append($$, string($3))
  }

bool_option_name:
  CONCAT_NULL_YIELDS_NULL
| CURSOR_CLOSE_ON_COMMIT
| QUOTED_IDENTIFIER
| ARITHABORT
| FMTONLY
| NOCOUNT
| NOEXEC
| NUMERIC_ROUNDABORT
| ANSI_DEFAULTS
| ANSI_NULL_DFLT_OFF
| ANSI_NULL_DFLT_ON
| ANSI_NULLS
| ANSI_PADDING
| ANSI_WARNINGS
| FORCEPLAN
| SHOWPLAN_ALL
| SHOWPLAN_TEXT
| SHOWPLAN_XML
| IMPLICIT_TRANSACTIONS
| REMOTE_PROC_TRANSACTIONS
| XACT_ABORT

grant_privilege_name:
  SELECT
  {
    $$ = string($1)
  }
| INSERT
  {
    $$ = string($1)
  }
| UPDATE
  {
    $$ = string($1)
  }
| DELETE
  {
    $$ = string($1)
  }
| TRUNCATE
  {
    $$ = string($1)
  }
| REFERENCES
  {
    $$ = string($1)
  }
| TRIGGER
  {
    $$ = string($1)
  }
| ALL
  {
    $$ = string($1)
  }
| ALL PRIVILEGES
  {
    $$ = "ALL PRIVILEGES"
  }
| sql_id
  {
    $$ = $1.String()
  }

grant_privileges:
  grant_privilege_name
  {
    $$ = []string{$1}
  }
| grant_privileges ',' grant_privilege_name
  {
    $$ = append($$, $3)
  }

grant_target_list:
  sql_id
  {
    $$ = []string{$1.String()}
  }
| grant_target_list ',' sql_id
  {
    $$ = append($$, $3.String())
  }

/*
 * These are not all necessarily reserved in MySQL, but some are.
 *
 * These are more importantly reserved because they may conflict with our grammar.
 *
 * Sorted alphabetically
 */
reserved_keyword:
  ADD
| AFTER
| ALWAYS
| AND
| AS
| ASC
| AUTO_INCREMENT
| AUTOINCREMENT
| BEFORE
| BETWEEN
| BINARY
| BY
| CASE
| CLOSE
| CLUSTERED
| NONCLUSTERED
| COLLATE
| COLUMNS_UPDATED
| CONVERT
| CREATE
| CROSS
| CURRENT_DATE
| CURRENT_TIME
| CURRENT_TIMESTAMP
| CURSOR
| SUBSTR
| SUBSTRING
| DATABASE
| DATABASES
| DEALLOCATE
| DECLARE
| DEFAULT
| DELETE
| DESC
| DESCRIBE
| DISTINCT
| DIV
| DROP
| EACH
| ELSE
| END
| ESCAPE
| EXEC
| EXECUTE
| EXISTS
| EXPLAIN
| FALSE
| FETCH
| FIRST
| FOR
| FORCE
| FOREIGN
| FROM
| GENERATED
| GROUP
| HAVING
| HOLDLOCK
| IDENTITY
| IF
| IGNORE
| IN
| INCLUDE
| INDEX
| INNER
| INSERT
| INTERVAL
| INTO
| IS
| JOIN
| KEY
| LAST
| LEFT
| LIKE
| LIMIT
| LOCALTIME
| LOCALTIMESTAMP
| LOCK
| MATCH
| MAXVALUE
| MOD
| NATURAL
| NEXT // next should be doable as non-reserved, but is not due to the special `select next num_val` query that the original parser supports
| NOLOCK
| NOT
| NOWAIT
| NULL
| ON
| ONLY
| OPEN
| OR
| ORDER
| OUTER
| PARTITION
| PAGLOCK
| PRIOR
| READUNCOMMITTED
| REGEXP
| RENAME
| REPLACE
| RETURN
| RIGHT
| ROW
| ROWLOCK
| SCHEMA
| SCROLL
| SELECT
| SEPARATOR
| SET
| SHOW
| STRAIGHT_JOIN
| TABLE
| TABLES
| TABLOCK
| THEN
| TO
| TRUE
| UNION
| UNIQUE
| UPDATE
| UPDLOCK
| USE
| USING
| UTC_DATE
| UTC_TIME
| UTC_TIMESTAMP
| VALUES
| WHEN
| WHERE
| WHILE
| WITH
| OFF

non_reserved_keyword:
  DATA
| DEFINER
| INVOKER
| POLICY
| TYPE
| STATUS
| VARIABLES
| ZONE
| CITEXT

openb:
  '('
  {
    if incNesting(yylex) {
      yylex.Error("max nesting level reached")
      return 1
    }
  }

closeb:
  ')'
  {
    decNesting(yylex)
  }

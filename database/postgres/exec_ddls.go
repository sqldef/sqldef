package postgres

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/lib/pq"
	"github.com/sqldef/sqldef/v3/database"
)

// ExecDDLs sends ddls as one simple query, which PostgreSQL stops at the first statement that
// fails. lib/pq reports nothing for a statement that returns no rows, so each one is followed by a
// SELECT of how many have run.
//
// PostgreSQL parses the whole query before it runs any of it, so a syntax error in any statement
// fails the query with none of them run. A savepoint keeps the transaction, and what ran in it
// before, so that they can run one at a time and the error belongs to the statement that has it.
func (d *PostgresDatabase) ExecDDLs(transaction *sql.Tx, ddls []string) (int, error) {
	if _, err := transaction.Exec("SAVEPOINT psqldef_ddls"); err != nil {
		return 0, err
	}

	var query strings.Builder
	// Where each statement starts in the query, in characters as PostgreSQL counts them
	starts := make([]int, len(ddls))
	length := 0
	for i, ddl := range ddls {
		starts[i] = length
		// The ";" goes on a line of its own, since a statement may end with a "--" comment
		part := fmt.Sprintf("%s\n;\nSELECT %d AS psqldef_done;\n", ddl, i+1)
		query.WriteString(part)
		length += utf8.RuneCountInString(part)
	}

	succeeded := 0
	rows, err := transaction.Query(query.String())
	if err == nil {
		for {
			for rows.Next() {
				if err := rows.Scan(&succeeded); err != nil {
					panic(err)
				}
			}
			if !rows.NextResultSet() {
				break
			}
		}
		err = rows.Err()
		rows.Close()
	}
	if err == nil {
		return succeeded, nil
	}

	var pqErr *pq.Error
	if succeeded == 0 && errors.As(err, &pqErr) && pqErr.Code == "42601" { // syntax_error
		if _, rollbackErr := transaction.Exec("ROLLBACK TO SAVEPOINT psqldef_ddls"); rollbackErr != nil {
			return 0, rollbackErr
		}
		return 0, database.ErrDDLsNotRun
	}
	// The connection can fail after the last statement has run
	if succeeded == len(ddls) {
		return succeeded, err
	}
	return succeeded, statementError(err, ddls[succeeded], starts[succeeded])
}

// statementError gives err the location lib/pq would write if statement, which starts at the
// character start of the query, had been the whole query.
func statementError(err error, statement string, start int) error {
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) || pqErr.Position == "" {
		return err
	}
	position, convErr := strconv.Atoi(pqErr.Position)
	if convErr != nil {
		return err
	}
	return &statementLocatedError{err: pqErr, location: locate(statement, position-start)}
}

// statementLocatedError is a *pq.Error whose message locates the error in its own statement.
type statementLocatedError struct {
	err      *pq.Error
	location string
}

// Error is (*pq.Error).Error with the location in the statement.
func (e *statementLocatedError) Error() string {
	message := e.err.Message + e.location
	if e.err.Code != "" {
		return "pq: " + message + " (" + string(e.err.Code) + ")"
	}
	return "pq: " + message
}

func (e *statementLocatedError) Unwrap() error {
	return e.err
}

// locate writes position, in characters from 1, as (*pq.Error).Error does.
func locate(statement string, position int) string {
	lines := strings.Split(statement, "\n")
	line, column, read := 0, 0, 0
	for _, text := range lines {
		line++
		length := utf8.RuneCountInString(text) + 1
		if read+length >= position {
			column = max(position-read, 1)
			break
		}
		read += length
	}
	if len(lines) == 1 {
		return " at column " + strconv.Itoa(column)
	}
	return " at position " + strconv.Itoa(line) + ":" + strconv.Itoa(column)
}

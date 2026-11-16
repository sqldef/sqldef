//go:build !windows

package postgres

import (
	"fmt"
	"strings"
	"testing"

	"github.com/sqldef/sqldef/v3/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sequentialDatabase hides ExecDDLs, so that RunDDLs runs the statements one at a time.
type sequentialDatabase struct {
	database.Database
}

type bufferLogger struct {
	strings.Builder
}

func (l *bufferLogger) Print(v ...any)                 { fmt.Fprint(&l.Builder, v...) }
func (l *bufferLogger) Printf(format string, v ...any) { fmt.Fprintf(&l.Builder, format, v...) }
func (l *bufferLogger) Println(v ...any)               { fmt.Fprintln(&l.Builder, v...) }

func TestRunDDLsInOneRoundTripAsOneAtATime(t *testing.T) {
	for _, test := range []struct {
		name        string
		setup       string
		beforeApply string
		ddls        []string
	}{
		{
			name:  "all succeed",
			setup: "CREATE TABLE users (id bigint)",
			ddls:  []string{"CREATE TABLE a (id int)", "-- Skipped: DROP TABLE x", "ALTER TABLE users ADD COLUMN name text", "COMMENT ON TABLE a IS 'a; b'"},
		},
		{
			name: "a statement ends with a comment",
			ddls: []string{"CREATE TABLE a (id int) -- note", "CREATE TABLE b (id int)"},
		},
		{
			name:  "the first statement fails",
			setup: "CREATE TABLE a (id int)",
			ddls:  []string{"CREATE TABLE a (id int)", "CREATE TABLE b (id int)"},
		},
		{
			name:  "a later statement fails",
			setup: "CREATE TABLE users (id bigint, name text); INSERT INTO users VALUES (1, NULL)",
			ddls:  []string{"CREATE TABLE a (id int)", "-- Skipped: DROP TABLE x", "ALTER TABLE users ALTER COLUMN name SET NOT NULL", "-- Skipped: DROP TABLE y", "CREATE TABLE b (id int)"},
		},
		{
			name: "a later statement fails with a location",
			ddls: []string{"CREATE TABLE a (id int)", "CREATE VIEW v AS SELECT 'é' AS e, nope FROM a"},
		},
		{
			name: "a later statement of several lines fails with a location",
			ddls: []string{"CREATE TABLE a (id int)", "CREATE VIEW v AS\n  SELECT 'é' AS e, nope\n  FROM a"},
		},
		{
			name: "a later statement has a syntax error",
			ddls: []string{"CREATE TABLE a (id int)", "CREATE TABL b (id int)", "CREATE TABLE c (id int)"},
		},
		{
			name: "a later statement is cut off",
			ddls: []string{"CREATE TABLE a (id int)", "CREATE TABLE b (id int"},
		},
		{
			name:        "a syntax error after before-apply",
			beforeApply: "CREATE TABLE before_apply (id int)",
			ddls:        []string{"CREATE TABLE a (id int)", "CREATE TABL b (id int)"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			run := func(batched bool) (string, string, string) {
				db := setupTestDatabase(t)
				defer db.Close()
				if test.setup != "" {
					_, err := db.DB().Exec(test.setup)
					require.NoError(t, err)
				}
				var target database.Database = sequentialDatabase{db}
				if batched {
					target = db
				}
				logger := &bufferLogger{}
				err := database.RunDDLs(target, test.ddls, test.beforeApply, "", logger)
				message := ""
				if err != nil {
					message = err.Error()
				}
				var columns string
				err = db.DB().QueryRow("SELECT coalesce(string_agg(table_name || '.' || column_name, ',' ORDER BY table_name, ordinal_position), '') FROM information_schema.columns WHERE table_schema = 'public'").Scan(&columns)
				require.NoError(t, err)
				return logger.String(), message, columns
			}
			output, message, columns := run(false)
			batchedOutput, batchedMessage, batchedColumns := run(true)
			assert.Equal(t, output, batchedOutput)
			assert.Equal(t, message, batchedMessage)
			assert.Equal(t, columns, batchedColumns)
		})
	}
}

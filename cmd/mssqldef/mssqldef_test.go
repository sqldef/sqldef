// Integration test of mssqldef command.
//
// Test requirement:
//   - go command
//   - `sqlcmd -Usa -PPassw0rd` must succeed
package main

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/sqldef/sqldef/cmd/testutils"
	"github.com/sqldef/sqldef/database"
	"github.com/sqldef/sqldef/database/mssql"
	"github.com/sqldef/sqldef/schema"
)

const (
	applyPrefix     = "-- Apply --\n"
	nothingModified = "-- Nothing is modified --\n"
)

func TestApply(t *testing.T) {
	tests, err := testutils.ReadTests("tests.yml")
	if err != nil {
		t.Fatal(err)
	}

	sqlParser := mssql.NewParser()
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			// Initialize the database with test.Current
			resetTestDatabase()
			var db database.Database
			var err error
			if test.User != "" {
				db, err = connectDatabaseByUser(test.User)
			} else {
				db, err = connectDatabase() // DROP DATABASE hangs when there's a connection
			}
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()

			testutils.RunTest(t, db, test, schema.GeneratorModeMssql, sqlParser, "")
		})
	}
}

// TODO: non-CLI tests should be migrated to TestApply

func TestMssqldefCreateTableQuotes(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE test_table (
		  id integer
		);
		GO
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE test_table (
		  id integer
		);
		GO
		`,
	)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMssqldefCreateTable(t *testing.T) {
	resetTestDatabase()

	createTable1 := stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name text,
		  age integer
		);
		GO
		`,
	)
	createTable2 := stripHeredoc(`
		CREATE TABLE bigdata (
		  data bigint
		);
		GO
		`,
	)

	assertApplyOutput(t, createTable1+createTable2, applyPrefix+createTable1+createTable2)
	assertApplyOutput(t, createTable1+createTable2, nothingModified)
	assertApplyOutput(t, createTable1, applyPrefix+"-- Skipped: DROP TABLE [dbo].[bigdata];\n")
}

func TestMssqldefCreateTableWithDefault(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  profile varchar(50) NOT NULL DEFAULT (''),
		  default_int int default ((20)),
		  default_bool bit default ((1)),
		  default_numeric numeric(5) default ((42.195)),
		  default_fixed_char varchar(3) default ('JPN'),
		  default_text text default (''),
		  default_date datetimeoffset default (getdate())
		);
		GO
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMssqldefCreateTableWithIDENTITY(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id integer PRIMARY KEY IDENTITY(1,1),
		  name text,
		  age integer
		);
		GO
		`,
	)

	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMssqldefCreateTableWithCLUSTERED(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id integer,
		  name text,
		  age integer,
		  CONSTRAINT PK_users PRIMARY KEY CLUSTERED (id)
		);
		GO
		`,
	)

	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMssqldefCreateView(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE [dbo].[users] (
		  id integer NOT NULL,
		  name text,
		  age integer
		);
		GO
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createView := "CREATE VIEW [dbo].[view_users] AS select id from dbo.users with(nolock) where age = 1;\nGO\n"
	assertApplyOutput(t, createTable+createView, applyPrefix+createView)
	assertApplyOutput(t, createTable+createView, nothingModified)

	createView = "CREATE VIEW [dbo].[view_users] AS select id from dbo.users with(nolock) where age = 2;\nGO\n"
	dropView := "DROP VIEW [dbo].[view_users];\nGO\n"
	assertApplyOutput(t, createTable+createView, applyPrefix+dropView+createView)
	assertApplyOutput(t, createTable+createView, nothingModified)
	assertApplyOutput(t, "", applyPrefix+"-- Skipped: DROP TABLE [dbo].[users];\n"+dropView)
}

func TestMssqldefTrigger(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name text
		);
		GO
		CREATE TABLE logs (
		  id bigint NOT NULL,
		  log varchar(20),
		  dt datetime
		);
		GO
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTrigger := stripHeredoc(`
		CREATE TRIGGER [insert_log] ON [dbo].[users] for insert AS
		insert into logs(log, dt) values ('insert', getdate());
		GO
		`,
	)
	assertApplyOutput(t, createTable+createTrigger, applyPrefix+createTrigger)
	assertApplyOutput(t, createTable+createTrigger, nothingModified)

	createTrigger = stripHeredoc(`
		CREATE TRIGGER [insert_log] ON [dbo].[users] for insert AS
		  delete from logs
		  insert into logs(log, dt) values ('insert', getdate());
		GO
		`,
	)

	assertApplyOutput(t, createTable+createTrigger, applyPrefix+stripHeredoc(`
		CREATE OR ALTER TRIGGER [insert_log] ON [dbo].[users] for insert AS
		delete from logs
		insert into logs(log, dt) values ('insert', getdate());
		GO
		`,
	))
	assertApplyOutput(t, createTable+createTrigger, nothingModified)
}

func TestMssqldefTriggerWithRichSyntax(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name text
		);
		GO
		CREATE TABLE logs (
		  id bigint NOT NULL,
		  log varchar(20),
		  dt datetime
		);
		GO
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTrigger := stripHeredoc(`
		CREATE TRIGGER [insert_log] ON [dbo].[users] after insert, delete AS
			declare
				@userId bigint,
				@username varchar(20),
				@date datetime
			select @userId = id from inserted
			set @date = getdate()
			declare
				users_cursor scroll cursor for
					select name from users order by id asc
			open users_cursor
			while @@FETCH_STATUS = 0
			begin
				fetch first from users_cursor into @username
				if @username = 'test'
				begin
					insert into logs(log, dt) values (@username, @date)
				end
				else
				begin
					insert into logs(log, dt) values ('insert user', @date)
				end
			end
			close users_cursor
			deallocate users_cursor
			insert into logs(log, dt) values (@username, @date);
			GO
		`,
	)
	assertApplyOutput(t, createTable+createTrigger, applyPrefix+createTrigger)
	assertApplyOutput(t, createTable+createTrigger, nothingModified)
}

func TestMssqldefAddColumn(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id BIGINT NOT NULL PRIMARY KEY
		);
		GO
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id BIGINT NOT NULL PRIMARY KEY,
		  name varchar(40)
		);
		GO`,
	)
	assertApplyOutput(t, createTable, applyPrefix+"ALTER TABLE [dbo].[users] ADD [name] varchar(40);\nGO\n")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMssqldefAddColumnWithIDENTITY(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id BIGINT NOT NULL PRIMARY KEY
		);
		GO
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id BIGINT NOT NULL PRIMARY KEY,
		  membership_id int IDENTITY(1,1) NOT FOR REPLICATION
		);
		GO`,
	)
	assertApplyOutput(t, createTable, applyPrefix+"ALTER TABLE [dbo].[users] ADD [membership_id] int IDENTITY(1,1) NOT FOR REPLICATION;\nGO\n")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMssqldefAddColumnWithCheck(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id BIGINT NOT NULL PRIMARY KEY
		);
		GO
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id BIGINT NOT NULL PRIMARY KEY,
		  membership_id int CHECK NOT FOR REPLICATION (membership_id>(0))
		);
		GO`,
	)
	assertApplyOutput(t, createTable, applyPrefix+"ALTER TABLE [dbo].[users] ADD [membership_id] int CHECK NOT FOR REPLICATION (membership_id > (0));\nGO\n")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMssqldefCreateTableDropColumn(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL PRIMARY KEY,
		  name varchar(20)
		);
		GO`,
	)
	assertApply(t, createTable)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL PRIMARY KEY
		);
		GO`,
	)
	assertApplyOutput(t, createTable, applyPrefix+"ALTER TABLE [dbo].[users] DROP COLUMN [name];\nGO\n")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMssqldefCreateTableDropColumnWithDefaultConstraint(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL PRIMARY KEY,
		  name varchar(20) CONSTRAINT df_name DEFAULT NULL
		);
		GO`,
	)
	assertApply(t, createTable)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL PRIMARY KEY
		);
		GO`,
	)
	assertApplyOutput(t, createTable, applyPrefix+"ALTER TABLE [dbo].[users] DROP CONSTRAINT [df_name];\nGO\n"+"ALTER TABLE [dbo].[users] DROP COLUMN [name];\nGO\n")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMssqldefCreateTableDropColumnWithDefault(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL PRIMARY KEY,
		  name varchar(20) DEFAULT NULL
		);
		GO`,
	)
	assertApply(t, createTable)

	// extract name of default constraint from sql server
	out, err := testutils.Execute("sqlcmd", "-Usa", "-PPassw0rd", "-dmssqldef_test", "-h", "-1", "-Q", stripHeredoc(`
		SELECT OBJECT_NAME(c.default_object_id) FROM sys.columns c WHERE c.object_id = OBJECT_ID('dbo.users', 'U') AND c.default_object_id != 0;
		`,
	))
	if err != nil {
		t.Error("failed to extract default object id")
	}
	dfConstraintName := strings.Replace((strings.Split(out, "\n")[0]), " ", "", -1)
	dropConstraint := fmt.Sprintf("ALTER TABLE [dbo].[users] DROP CONSTRAINT [%s];\nGO\n", dfConstraintName)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL PRIMARY KEY
		);`,
	)
	assertApplyOutput(t, createTable, applyPrefix+dropConstraint+"ALTER TABLE [dbo].[users] DROP COLUMN [name];\nGO\n")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMssqldefCreateTableDropColumnWithPK(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name varchar(20) DEFAULT NULL,
			CONSTRAINT pk_id PRIMARY KEY (id)
		);`,
	)
	assertApply(t, createTable)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  name varchar(20) DEFAULT NULL
		);`,
	)
	assertApplyOutput(t, createTable, applyPrefix+"ALTER TABLE [dbo].[users] DROP CONSTRAINT [pk_id];\nGO\n"+"ALTER TABLE [dbo].[users] DROP COLUMN [id];\nGO\n")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMssqldefCreateTableAddPrimaryKey(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name varchar(20)
		);
		GO
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL PRIMARY KEY,
		  name varchar(20)
		);
		GO
		`,
	)

	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE [dbo].[users] ADD PRIMARY KEY CLUSTERED ([id]);\nGO\n",
	)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMssqldefCreateTableAddPrimaryKeyConstraint(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name varchar(20)
		);
		GO
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name varchar(20),
		  CONSTRAINT [pk_users] PRIMARY KEY CLUSTERED ([id] desc)
		);
		GO
		`,
	)

	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE [dbo].[users] ADD CONSTRAINT [pk_users] PRIMARY KEY CLUSTERED ([id] desc);\nGO\n",
	)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMssqldefCreateTableDropPrimaryKey(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL PRIMARY KEY,
		  name varchar(20)
		);
		GO`,
	)
	assertApply(t, createTable)

	// extract name of primary key constraint from sql server
	out, err := testutils.Execute("sqlcmd", "-Usa", "-PPassw0rd", "-dmssqldef_test", "-h", "-1", "-Q", stripHeredoc(`
		SELECT kc.name FROM sys.key_constraints kc WHERE kc.parent_object_id=OBJECT_ID('users', 'U') AND kc.[type]='PK';
		`,
	))
	if err != nil {
		t.Error("failed to extract primary key id")
	}
	pkConstraintName := strings.Replace((strings.Split(out, "\n")[0]), " ", "", -1)
	dropConstraint := fmt.Sprintf("ALTER TABLE [dbo].[users] DROP CONSTRAINT [%s];\nGO\n", pkConstraintName)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name varchar(20)
		);
		GO`,
	)
	assertApplyOutput(t, createTable, applyPrefix+dropConstraint)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMssqldefCreateTableDropPrimaryKeyConstraint(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name varchar(20),
		  CONSTRAINT [pk_users] PRIMARY KEY ([id])
		);
		GO`,
	)
	assertApply(t, createTable)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name varchar(20)
		);
		GO`,
	)
	assertApplyOutput(t, createTable, applyPrefix+"ALTER TABLE [dbo].[users] DROP CONSTRAINT [pk_users];\nGO\n")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMssqldefCreateTableWithIndexOption(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name varchar(20),
		  INDEX [ix_users_id] UNIQUE CLUSTERED ([id]) WITH (
		    PAD_INDEX = ON,
		    FILLFACTOR = 10,
		    IGNORE_DUP_KEY = ON,
		    STATISTICS_NORECOMPUTE = ON,
		    STATISTICS_INCREMENTAL = OFF,
		    ALLOW_ROW_LOCKS = ON,
		    ALLOW_PAGE_LOCKS = ON
		  )
		);
		GO
		`)

	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMssqldefCreateTablePrimaryKeyWithIndexOption(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name varchar(20),
		  CONSTRAINT [pk_users] PRIMARY KEY CLUSTERED ([id]) WITH (
		    PAD_INDEX = OFF,
		    STATISTICS_NORECOMPUTE = OFF,
		    IGNORE_DUP_KEY = OFF,
		    ALLOW_ROW_LOCKS = ON,
		    ALLOW_PAGE_LOCKS = ON
		  )
		);
		GO
		`)

	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMssqldefCreateTableAddIndex(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name varchar(20)
		);
		GO
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name varchar(20),
		  INDEX [ix_users_id] UNIQUE CLUSTERED ([id] desc) WITH (
		    PAD_INDEX = ON,
		    FILLFACTOR = 10,
		    STATISTICS_NORECOMPUTE = ON
		  ) ON [PRIMARY]
		);
		GO
		`,
	)

	assertApplyOutput(t, createTable, applyPrefix+
		"CREATE UNIQUE CLUSTERED INDEX [ix_users_id] ON [dbo].[users] ([id] desc) WITH (pad_index = ON, fillfactor = 10, statistics_norecompute = ON) ON [PRIMARY];\nGO\n",
	)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMssqldefCreateTableDropIndex(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name varchar(20),
		  INDEX [ix_users_id] UNIQUE CLUSTERED ([id]) WITH (
		    PAD_INDEX = ON,
		    FILLFACTOR = 10,
		    STATISTICS_NORECOMPUTE = ON
		  )
		);
		GO
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name varchar(20)
		);
		GO
		`,
	)

	assertApplyOutput(t, createTable, applyPrefix+"DROP INDEX [ix_users_id] ON [dbo].[users];\nGO\n")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMssqldefCreateTableChangeIndexDefinition(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name varchar(20),
		  INDEX [ix_users_id] UNIQUE CLUSTERED ([id] desc)
		);
		GO
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name varchar(20),
		  INDEX [ix_users_id] UNIQUE CLUSTERED ([id] asc)
		);
		GO
		`,
	)

	assertApplyOutput(t, createTable, applyPrefix+
		"DROP INDEX [ix_users_id] ON [dbo].[users];\nGO\n"+
		"CREATE UNIQUE CLUSTERED INDEX [ix_users_id] ON [dbo].[users] ([id]);\nGO\n",
	)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name varchar(20),
		  INDEX [ix_users_id] UNIQUE CLUSTERED ([id]) WITH (
		    PAD_INDEX = ON,
		    FILLFACTOR = 10
		  )
		);
		GO
		`,
	)

	assertApplyOutput(t, createTable, applyPrefix+
		"DROP INDEX [ix_users_id] ON [dbo].[users];\nGO\n"+
		"CREATE UNIQUE CLUSTERED INDEX [ix_users_id] ON [dbo].[users] ([id]) WITH (pad_index = ON, fillfactor = 10);\nGO\n",
	)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMssqldefCreateTableForeignKey(t *testing.T) {
	resetTestDatabase()

	createUsers := "CREATE TABLE users (id BIGINT PRIMARY KEY);\nGO\n"
	createPosts := stripHeredoc(`
		CREATE TABLE posts (
		  content text,
		  user_id bigint
		);
		GO
		`,
	)
	assertApplyOutput(t, createUsers+createPosts, applyPrefix+createUsers+createPosts)
	assertApplyOutput(t, createUsers+createPosts, nothingModified)

	createPosts = stripHeredoc(`
		CREATE TABLE posts (
		  content text,
		  user_id bigint,
		  CONSTRAINT posts_ibfk_1 FOREIGN KEY (user_id) REFERENCES users (id)
		);
		GO
		`,
	)
	assertApplyOutput(t, createUsers+createPosts, applyPrefix+"ALTER TABLE [dbo].[posts] ADD CONSTRAINT [posts_ibfk_1] FOREIGN KEY ([user_id]) REFERENCES [dbo].[users] ([id]);\nGO\n")
	assertApplyOutput(t, createUsers+createPosts, nothingModified)

	createPosts = stripHeredoc(`
		CREATE TABLE posts (
		  content text,
		  user_id bigint,
		  CONSTRAINT posts_ibfk_1 FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE SET NULL ON UPDATE CASCADE
		);
		GO
		`,
	)
	assertApplyOutput(t, createUsers+createPosts, applyPrefix+
		"ALTER TABLE [dbo].[posts] DROP CONSTRAINT [posts_ibfk_1];\nGO\n"+
		"ALTER TABLE [dbo].[posts] ADD CONSTRAINT [posts_ibfk_1] FOREIGN KEY ([user_id]) REFERENCES [dbo].[users] ([id]) ON DELETE SET NULL ON UPDATE CASCADE;\nGO\n",
	)
	assertApplyOutput(t, createUsers+createPosts, nothingModified)

	createPosts = stripHeredoc(`
		CREATE TABLE posts (
		  content text,
		  user_id bigint
		);
		`,
	)
	assertApplyOutput(t, createUsers+createPosts, applyPrefix+"ALTER TABLE [dbo].[posts] DROP CONSTRAINT [posts_ibfk_1];\nGO\n")
	assertApplyOutput(t, createUsers+createPosts, nothingModified)
}

func TestMssqldefCreateTableWithCheck(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE a (
		  a_id INTEGER PRIMARY KEY CONSTRAINT [a_a_id_check] CHECK ([a_id]>(0)),
		  my_text TEXT NOT NULL
		);
		GO
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE a (
		  a_id INTEGER PRIMARY KEY CONSTRAINT [a_a_id_check] CHECK ([a_id]>(1)),
		  my_text TEXT NOT NULL
		);
		GO
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE [dbo].[a] DROP CONSTRAINT a_a_id_check;\nGO\n"+
		"ALTER TABLE [dbo].[a] ADD CONSTRAINT a_a_id_check CHECK (a_id > (1));\nGO\n")
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE a (
		  a_id INTEGER PRIMARY KEY,
		  my_text TEXT NOT NULL
		);
		GO
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE [dbo].[a] DROP CONSTRAINT a_a_id_check;\nGO\n")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMssqldefCreateTableWithCheckWithoutName(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE a (
		  a_id INTEGER PRIMARY KEY CHECK ([a_id]>(0)),
		  my_text TEXT NOT NULL
		);
		GO
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE a (
		  a_id INTEGER PRIMARY KEY CHECK ([a_id]>(1)),
		  my_text TEXT NOT NULL
		);
		GO
		`,
	)

	// extract name of check constraint from sql server
	out, err := testutils.Execute("sqlcmd", "-Usa", "-PPassw0rd", "-dmssqldef_test", "-h", "-1", "-Q", stripHeredoc(`
		SELECT name FROM sys.check_constraints cc WHERE cc.parent_object_id = OBJECT_ID('dbo.a', 'U');
		`,
	))
	if err != nil {
		t.Error("failed to extract check constraint name")
	}
	checkConstraintName := strings.Replace((strings.Split(out, "\n")[0]), " ", "", -1)
	dropConstraint := fmt.Sprintf("ALTER TABLE [dbo].[a] DROP CONSTRAINT %s;\nGO\n", checkConstraintName)

	assertApplyOutput(t, createTable, applyPrefix+
		dropConstraint+"ALTER TABLE [dbo].[a] ADD CONSTRAINT a_a_id_check CHECK (a_id > (1));\nGO\n")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMssqldefCreateIndex(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id BIGINT NOT NULL IDENTITY(1,1) PRIMARY KEY,
		  name varchar(40) DEFAULT NULL,
		  created_at datetime NOT NULL
		);
		GO
		`,
	)
	assertApply(t, createTable)

	createIndex := "CREATE NONCLUSTERED INDEX [index_name] ON [users] ([name] DESC) INCLUDE([created_at]) WITH (PAD_INDEX = ON) ON [PRIMARY];\nGO\n"
	assertApplyOutput(t, createTable+createIndex, applyPrefix+createIndex)
	assertApplyOutput(t, createTable+createIndex, nothingModified)

	assertApplyOutput(t, createTable, applyPrefix+"DROP INDEX [index_name] ON [dbo].[users];\nGO\n")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMssqldefCreateIndexChangeIndexDefinition(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id BIGINT NOT NULL IDENTITY(1,1) PRIMARY KEY,
		  name varchar(40) DEFAULT NULL,
		  created_at datetime NOT NULL,
		  updated_at datetime NOT NULL
		);
		GO
		`,
	)

	createIndex := "CREATE NONCLUSTERED INDEX [index_name] ON [users] ([name] DESC) INCLUDE([created_at]) WITH (PAD_INDEX = ON);\nGO\n"
	assertApplyOutput(t, createTable+createIndex, applyPrefix+createTable+createIndex)
	assertApplyOutput(t, createTable+createIndex, nothingModified)

	createIndex = "CREATE NONCLUSTERED INDEX [index_name] ON [users] ([name] DESC) INCLUDE([created_at], [updated_at]) WITH (PAD_INDEX = ON);\nGO\n"
	assertApplyOutput(t, createTable+createIndex, applyPrefix+
		"DROP INDEX [index_name] ON [dbo].[users];\nGO\n"+
		"CREATE NONCLUSTERED INDEX [index_name] ON [users] ([name] DESC) INCLUDE([created_at], [updated_at]) WITH (PAD_INDEX = ON);\nGO\n",
	)
	assertApplyOutput(t, createTable+createIndex, nothingModified)

	createIndex = "CREATE NONCLUSTERED INDEX [index_name] ON [users] ([name] DESC) INCLUDE([created_at], [updated_at]) WITH (PAD_INDEX = ON, FILLFACTOR = 10);\nGO\n"
	assertApplyOutput(t, createTable+createIndex, applyPrefix+
		"DROP INDEX [index_name] ON [dbo].[users];\nGO\n"+
		"CREATE NONCLUSTERED INDEX [index_name] ON [users] ([name] DESC) INCLUDE([created_at], [updated_at]) WITH (PAD_INDEX = ON, FILLFACTOR = 10);\nGO\n",
	)
	assertApplyOutput(t, createTable+createIndex, nothingModified)
}

func TestMssqldefCreateTableNotForReplication(t *testing.T) {
	resetTestDatabase()

	createUsers := "CREATE TABLE users (id BIGINT PRIMARY KEY);\nGO\n"
	createPosts := stripHeredoc(`
		CREATE TABLE posts (
		  post_id BIGINT IDENTITY(1,1) NOT FOR REPLICATION,
		  user_id BIGINT,
		  content TEXT,
		  views INTEGER CHECK NOT FOR REPLICATION ([views]>(-1)),
		  CONSTRAINT posts_ibfk_1 FOREIGN KEY (user_id) REFERENCES users (id) NOT FOR REPLICATION
		);
		GO
		`,
	)
	assertApplyOutput(t, createUsers+createPosts, applyPrefix+createUsers+createPosts)
	assertApplyOutput(t, createUsers+createPosts, nothingModified)
}

func TestMssqldefCreateTableAddNotForReplication(t *testing.T) {
	resetTestDatabase()

	createUsers := "CREATE TABLE users (id BIGINT PRIMARY KEY);\nGO\n"
	createPosts := stripHeredoc(`
		CREATE TABLE posts (
		  post_id BIGINT IDENTITY(1,1),
		  user_id BIGINT,
		  content TEXT,
		  views INTEGER CONSTRAINT posts_view_check CHECK ([views]>(-1)),
		  CONSTRAINT posts_ibfk_1 FOREIGN KEY (user_id) REFERENCES users (id)
		);
		GO
		`,
	)
	assertApplyOutput(t, createUsers+createPosts, applyPrefix+createUsers+createPosts)
	assertApplyOutput(t, createUsers+createPosts, nothingModified)

	createPosts = stripHeredoc(`
		CREATE TABLE posts (
		  post_id BIGINT IDENTITY(1,1) NOT FOR REPLICATION,
		  user_id BIGINT,
		  content TEXT,
		  views INTEGER CONSTRAINT posts_view_check CHECK NOT FOR REPLICATION ([views]>(-1)),
		  CONSTRAINT posts_ibfk_1 FOREIGN KEY (user_id) REFERENCES users (id) NOT FOR REPLICATION
		);
		GO
		`,
	)
	assertApplyOutput(t, createUsers+createPosts, applyPrefix+
		"ALTER TABLE [dbo].[posts] DROP COLUMN [post_id];\nGO\n"+
		"ALTER TABLE [dbo].[posts] ADD [post_id] bigint IDENTITY(1,1) NOT FOR REPLICATION;\nGO\n"+
		"ALTER TABLE [dbo].[posts] DROP CONSTRAINT posts_view_check;\nGO\n"+
		"ALTER TABLE [dbo].[posts] ADD CONSTRAINT posts_view_check CHECK NOT FOR REPLICATION (views > (-1));\nGO\n"+
		"ALTER TABLE [dbo].[posts] DROP CONSTRAINT [posts_ibfk_1];\nGO\n"+
		"ALTER TABLE [dbo].[posts] ADD CONSTRAINT [posts_ibfk_1] FOREIGN KEY ([user_id]) REFERENCES [dbo].[users] ([id]) NOT FOR REPLICATION;\nGO\n",
	)
	assertApplyOutput(t, createUsers+createPosts, nothingModified)
}

func TestMssqldefCreateTableAddDefaultChangeDefault(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE messages (
		  id BIGINT NOT NULL PRIMARY KEY,
		  content TEXT NOT NULL
		);
		GO
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE messages (
		  id BIGINT NOT NULL PRIMARY KEY,
		  content TEXT NOT NULL CONSTRAINT [df_messages_content] DEFAULT ''
		);
		GO
		`,
	)

	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE [dbo].[messages] ADD CONSTRAINT [df_messages_content] DEFAULT '' FOR [content];\nGO\n",
	)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE messages (
		  id BIGINT NOT NULL PRIMARY KEY,
		  content TEXT NOT NULL DEFAULT 'hello'
		);
		GO
		`,
	)

	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE [dbo].[messages] DROP CONSTRAINT [df_messages_content];\nGO\n"+
		"ALTER TABLE [dbo].[messages] ADD DEFAULT 'hello' FOR [content];\nGO\n",
	)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMssqldefUseSequenceInTrigger(t *testing.T) {
	resetTestDatabase()

	// Prepare sequence, because "CREATE SEQUENCE" is currently unavailable in mssqldef.
	testutils.MustExecute("sqlcmd", "-Usa", "-PPassw0rd", "-dmssqldef_test", "-Q", stripHeredoc(`
		CREATE SEQUENCE seq_user_id AS int START WITH 1;
		GO
	`))

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id int NOT NULL PRIMARY KEY,
		  name text,
		  age int
		);
		GO
	`)
	createTrigger := stripHeredoc(`
		CREATE TRIGGER [insert_users_trigger] ON [dbo].[users] instead of insert AS
		set nocount on
		insert into dbo.users(id, name, age) select next value for seq_user_id, name, age from inserted;
		GO
	`)

	assertApplyOutput(t, createTable+createTrigger, applyPrefix+createTable+createTrigger)
	assertApplyOutput(t, createTable+createTrigger, nothingModified)
}

//
// ----------------------- following tests are for CLI -----------------------
//

func TestMssqldefApply(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE bigdata (
		  data bigint
		);
		GO
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMssqldefDryRun(t *testing.T) {
	resetTestDatabase()
	writeFile("schema.sql", stripHeredoc(`
		CREATE TABLE users (
		  id integer NOT NULL PRIMARY KEY,
		  age integer
		);
		GO
		`,
	))

	dryRun := assertedExecute(t, "./mssqldef", "-Usa", "-PPassw0rd", "mssqldef_test", "--dry-run", "--file", "schema.sql")
	apply := assertedExecute(t, "./mssqldef", "-Usa", "-PPassw0rd", "mssqldef_test", "--file", "schema.sql")
	assertEquals(t, dryRun, strings.Replace(apply, "Apply", "dry run", 1))
}

func TestMssqldefDropTable(t *testing.T) {
	resetTestDatabase()
	testutils.MustExecute("sqlcmd", "-Usa", "-PPassw0rd", "-dmssqldef_test", "-Q", stripHeredoc(`
		CREATE TABLE users (
		    id integer NOT NULL PRIMARY KEY,
		    age integer
		);
		GO
		`,
	))

	writeFile("schema.sql", "")

	dropTable := stripHeredoc(`
		DROP TABLE [dbo].[users];
		GO
		`,
	)
	out := assertedExecute(t, "./mssqldef", "-Usa", "-PPassw0rd", "mssqldef_test", "--enable-drop-table", "--file", "schema.sql")
	assertEquals(t, out, applyPrefix+dropTable)
}

func TestMssqldefExport(t *testing.T) {
	resetTestDatabase()
	out := assertedExecute(t, "./mssqldef", "-Usa", "-PPassw0rd", "mssqldef_test", "--export")
	assertEquals(t, out, "-- No table exists --\n")

	testutils.MustExecute("sqlcmd", "-Usa", "-PPassw0rd", "-dmssqldef_test", "-Q", stripHeredoc(`
		CREATE TABLE dbo.v (
		    v_int int NOT NULL,
		    v_smallmoney smallmoney,
		    v_money money,
		    v_datetimeoffset datetimeoffset(1),
		    v_datetime2 datetime2,
		    v_smalldatetime smalldatetime,
		    v_nchar nchar(30),
		    v_varchar varchar(30),
		    v_nvarchar nvarchar(50)
		);
		`,
	))
	out = assertedExecute(t, "./mssqldef", "-Usa", "-PPassw0rd", "mssqldef_test", "--export")
	assertEquals(t, out, stripHeredoc(`
		CREATE TABLE dbo.v (
		    [v_int] int NOT NULL,
		    [v_smallmoney] smallmoney,
		    [v_money] money,
		    [v_datetimeoffset] datetimeoffset(1),
		    [v_datetime2] datetime2,
		    [v_smalldatetime] smalldatetime,
		    [v_nchar] nchar(30),
		    [v_varchar] varchar(30),
		    [v_nvarchar] nvarchar(50)
		);
		GO
		`,
	))
}

func TestMssqldefExportConstraint(t *testing.T) {
	resetTestDatabase()

	sql := stripHeredoc(`
		CREATE TABLE dbo.v (
		    [v1] int NOT NULL,
		    [v2] int NOT NULL,
		    CONSTRAINT [v_pk] PRIMARY KEY CLUSTERED ([v1], [v2]) WITH ( PAD_INDEX = OFF, IGNORE_DUP_KEY = OFF, STATISTICS_NORECOMPUTE = OFF, STATISTICS_INCREMENTAL = OFF, ALLOW_ROW_LOCKS = ON, ALLOW_PAGE_LOCKS = ON )
		);
	`,
	)

	testutils.MustExecute("sqlcmd", "-Usa", "-PPassw0rd", "-dmssqldef_test", "-Q", sql)

	out := assertedExecute(t, "./mssqldef", "-Usa", "-PPassw0rd", "mssqldef_test", "--export")
	assertEquals(t, out, sql+"GO\n")
}

func TestMssqldefHelp(t *testing.T) {
	_, err := testutils.Execute("./mssqldef", "--help")
	if err != nil {
		t.Errorf("failed to run --help: %s", err)
	}

	out, err := testutils.Execute("./mssqldef")
	if err == nil {
		t.Errorf("no database must be error, but successfully got: %s", out)
	}
}

func TestMain(m *testing.M) {
	resetTestDatabase()
	testutils.MustExecute("go", "build")
	status := m.Run()
	_ = os.Remove("mssqldef")
	_ = os.Remove("schema.sql")
	os.Exit(status)
}

func assertApply(t *testing.T, schema string) {
	t.Helper()
	writeFile("schema.sql", schema)
	assertedExecute(t, "./mssqldef", "-Usa", "-PPassw0rd", "mssqldef_test", "--file", "schema.sql")
}

func assertApplyOutput(t *testing.T, schema string, expected string) {
	t.Helper()
	writeFile("schema.sql", schema)
	actual := assertedExecute(t, "./mssqldef", "-Usa", "-PPassw0rd", "mssqldef_test", "--file", "schema.sql")
	assertEquals(t, actual, expected)
}

func assertedExecute(t *testing.T, command string, args ...string) string {
	t.Helper()
	out, err := testutils.Execute(command, args...)
	if err != nil {
		t.Errorf("failed to execute '%s %s' (error: '%s'): `%s`", command, strings.Join(args, " "), err, out)
	}
	return out
}

func assertEquals(t *testing.T, actual string, expected string) {
	t.Helper()

	if expected != actual {
		t.Errorf("expected '%s' but got '%s'", expected, actual)
	}
}

func resetTestDatabase() {
	testutils.MustExecute("sqlcmd", "-Usa", "-PPassw0rd", "-Q", "IF NOT EXISTS (SELECT name FROM sys.database_principals WHERE name = 'mssqldef_user') BEGIN CREATE LOGIN mssqldef_user WITH PASSWORD = N'Passw0rd' END;")
	testutils.MustExecute("sqlcmd", "-Usa", "-PPassw0rd", "-Q", "DROP DATABASE IF EXISTS mssqldef_test;")
	testutils.MustExecute("sqlcmd", "-Usa", "-PPassw0rd", "-Q", "CREATE DATABASE mssqldef_test;")
	testutils.MustExecute("sqlcmd", "-Usa", "-PPassw0rd", "-dmssqldef_test", "-Q", "CREATE SCHEMA FOO;")
	testutils.MustExecute("sqlcmd", "-Usa", "-PPassw0rd", "-dmssqldef_test", "-Q", "CREATE USER mssqldef_user FOR LOGIN mssqldef_user WITH DEFAULT_SCHEMA = FOO;")
	testutils.MustExecute("sqlcmd", "-Usa", "-PPassw0rd", "-dmssqldef_test", "-Q", "ALTER ROLE db_owner ADD MEMBER mssqldef_user;")
	testutils.MustExecute("sqlcmd", "-Usa", "-PPassw0rd", "-dmssqldef_test", "-Q", "ALTER AUTHORIZATION ON SCHEMA::FOO TO mssqldef_user;")
}

func writeFile(path string, content string) {
	file, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	file.Write(([]byte)(content))
}

func stripHeredoc(heredoc string) string {
	heredoc = strings.TrimPrefix(heredoc, "\n")
	re := regexp.MustCompilePOSIX("^\t*")
	return re.ReplaceAllLiteralString(heredoc, "")
}

func connectDatabase() (database.Database, error) {
	return mssql.NewDatabase(database.Config{
		User:     "sa",
		Password: "Passw0rd",
		Host:     "127.0.0.1",
		Port:     1433,
		DbName:   "mssqldef_test",
	})
}

func connectDatabaseByUser(user string) (database.Database, error) {
	return mssql.NewDatabase(database.Config{
		User:     user,
		Password: "Passw0rd",
		Host:     "127.0.0.1",
		Port:     1433,
		DbName:   "mssqldef_test",
	})
}

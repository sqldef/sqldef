package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"syscall"

	"github.com/sqldef/sqldef/database/file"

	"github.com/jessevdk/go-flags"
	"github.com/sqldef/sqldef"
	"github.com/sqldef/sqldef/database"
	"github.com/sqldef/sqldef/database/postgres"
	"github.com/sqldef/sqldef/schema"
	"golang.org/x/term"
)

var version string

// Return parsed options and schema filename
// TODO: Support `sqldef schema.sql -opt val...`
func parseOptions(args []string) (database.Config, *sqldef.Options) {
	var opts struct {
		User            string   `short:"U" long:"user" description:"PostgreSQL user name" value-name:"username" default:"postgres"`
		Password        string   `short:"W" long:"password" description:"PostgreSQL user password, overridden by $PGPASSWORD" value-name:"password"`
		Host            string   `short:"h" long:"host" description:"Host or socket directory to connect to the PostgreSQL server" value-name:"hostname" default:"127.0.0.1"`
		Port            uint     `short:"p" long:"port" description:"Port used for the connection" value-name:"port" default:"5432"`
		Prompt          bool     `long:"password-prompt" description:"Force PostgreSQL user password prompt"`
		File            []string `short:"f" long:"file" description:"Read desired SQL from the file, rather than stdin" value-name:"filename" default:"-"`
		DryRun          bool     `long:"dry-run" description:"Don't run DDLs but just show them"`
		Export          bool     `long:"export" description:"Just dump the current schema to stdout"`
		EnableDropTable bool     `long:"enable-drop-table" description:"Enable destructive changes such as DROP (enable only table drops)"`
		SkipView        bool     `long:"skip-view" description:"Skip managing views/materialized views"`
		SkipExtension   bool     `long:"skip-extension" description:"Skip managing extensions"`
		BeforeApply     string   `long:"before-apply" description:"Execute the given string before applying the regular DDLs"`
		Config          string   `long:"config" description:"YAML file to specify: target_tables, skip_tables, target_schema"`
		Help            bool     `long:"help" description:"Show this help"`
		Version         bool     `long:"version" description:"Show this version"`
	}

	parser := flags.NewParser(&opts, flags.None)
	parser.Usage = "[OPTION]... [DBNAME|current.sql] < desired.sql"
	args, err := parser.ParseArgs(args)
	if err != nil {
		log.Fatal(err)
	}

	if opts.Help {
		parser.WriteHelp(os.Stdout)
		os.Exit(0)
	}

	if opts.Version {
		fmt.Println(version)
		os.Exit(0)
	}

	desiredFiles := sqldef.ParseFiles(opts.File)

	var desiredDDLs string
	if !opts.Export {
		desiredDDLs, err = sqldef.ReadFiles(desiredFiles)
		if err != nil {
			log.Fatalf("Failed to read '%v': %s", desiredFiles, err)
		}
	}

	options := sqldef.Options{
		DesiredDDLs:     desiredDDLs,
		DryRun:          opts.DryRun,
		Export:          opts.Export,
		EnableDropTable: opts.EnableDropTable,
		BeforeApply:     opts.BeforeApply,
		Config:          database.ParseGeneratorConfig(opts.Config),
	}

	if len(args) == 0 {
		fmt.Print("No database is specified!\n\n")
		parser.WriteHelp(os.Stdout)
		os.Exit(1)
	} else if len(args) > 1 {
		fmt.Printf("Multiple databases are given: %v\n\n", args)
		parser.WriteHelp(os.Stdout)
		os.Exit(1)
	}
	var databaseName string
	if strings.HasSuffix(args[0], ".sql") {
		options.CurrentFile = args[0]
	} else {
		databaseName = args[0]
	}

	password, ok := os.LookupEnv("PGPASSWORD")
	if !ok {
		password = opts.Password
	}

	if opts.Prompt {
		fmt.Printf("Enter Password: ")
		pass, err := term.ReadPassword(int(syscall.Stdin))
		if err != nil {
			log.Fatal(err)
		}
		password = string(pass)
	}

	config := database.Config{
		DbName:          databaseName,
		User:            opts.User,
		Password:        password,
		Host:            opts.Host,
		Port:            int(opts.Port),
		SkipView:        opts.SkipView,
		SkipExtension:   opts.SkipExtension,
		TargetSchema:    options.Config.TargetSchema,
		DumpConcurrency: options.Config.DumpConcurrency,
	}
	if _, err := os.Stat(config.Host); !os.IsNotExist(err) {
		config.Socket = config.Host
	}
	return config, &options
}

func main() {
	config, options := parseOptions(os.Args[1:])

	var db database.Database
	if len(options.CurrentFile) > 0 {
		db = file.NewDatabase(options.CurrentFile)
	} else {
		var err error
		db, err = postgres.NewDatabase(config)

		// Emulate the default behavior (sslmode=prefer) of psql when PGSSLMODE is not set,
		// which is not supported by Go's lib/pq.
		if _, ok := os.LookupEnv("PGSSLMODE"); !ok && err == nil {
			e := db.DB().Ping()
			if e != nil && strings.Contains(fmt.Sprintf("%s", e), "SSL is not enabled") {
				db.Close()
				os.Setenv("PGSSLMODE", "disable")
				db, err = postgres.NewDatabase(config)
			}
		}

		if err != nil {
			log.Fatal(err)
		}
		defer db.Close()
	}

	sqlParser := postgres.NewParser()
	sqldef.Run(schema.GeneratorModePostgres, db, sqlParser, options)
}

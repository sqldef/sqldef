package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"syscall"

	"github.com/sqldef/sqldef/v2/database/file"

	"github.com/jessevdk/go-flags"
	"github.com/sqldef/sqldef/v2"
	"github.com/sqldef/sqldef/v2/database"
	"github.com/sqldef/sqldef/v2/database/postgres"
	"github.com/sqldef/sqldef/v2/schema"
	"golang.org/x/term"
)

// version and revision are set via -ldflags
var version = "dev"
var revision = "HEAD"

// Return parsed options and schema filename
// TODO: Support `sqldef schema.sql -opt val...`
func parseOptions(args []string) (database.Config, *sqldef.Options) {
	// Track parsed configs in order
	var configs []database.GeneratorConfig

	var opts struct {
		User          string   `short:"U" long:"user" description:"PostgreSQL user name" value-name:"username" default:"postgres"`
		Password      string   `short:"W" long:"password" description:"PostgreSQL user password, overridden by $PGPASSWORD" value-name:"password"`
		Host          string   `short:"h" long:"host" description:"Host or socket directory to connect to the PostgreSQL server" value-name:"hostname" default:"127.0.0.1"`
		Port          uint     `short:"p" long:"port" description:"Port used for the connection" value-name:"port" default:"5432"`
		Prompt        bool     `long:"password-prompt" description:"Force PostgreSQL user password prompt"`
		File          []string `short:"f" long:"file" description:"Read desired SQL from the file, rather than stdin" value-name:"filename" default:"-"`
		DryRun        bool     `long:"dry-run" description:"Don't run DDLs but just show them"`
		Export        bool     `long:"export" description:"Just dump the current schema to stdout"`
		EnableDrop    bool     `long:"enable-drop" description:"Enable destructive changes such as DROP for TABLE, SCHEMA, ROLE, USER, FUNCTION, PROCEDURE, TRIGGER, VIEW, INDEX, SEQUENCE, TYPE"`
		SkipView      bool     `long:"skip-view" description:"Skip managing views/materialized views"`
		SkipExtension bool     `long:"skip-extension" description:"Skip managing extensions"`
		BeforeApply   string   `long:"before-apply" description:"Execute the given string before applying the regular DDLs"`
		Help          bool     `long:"help" description:"Show this help"`
		Version       bool     `long:"version" description:"Show this version"`

		// Custom handlers for config flags to preserve order
		Config       func(string) `long:"config" description:"YAML file to specify: target_tables, skip_tables, skip_views, target_schema, managed_roles (can be specified multiple times)"`
		ConfigInline func(string) `long:"config-inline" description:"YAML object to specify: target_tables, skip_tables, skip_views, target_schema, managed_roles (can be specified multiple times)"`
	}

	opts.Config = func(path string) {
		configs = append(configs, database.ParseGeneratorConfig(path))
	}
	opts.ConfigInline = func(yaml string) {
		configs = append(configs, database.ParseGeneratorConfigString(yaml))
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
		fmt.Printf("%s (%s)\n", version, revision)
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

	// merge --config and --config-inline in order
	config := database.MergeGeneratorConfigs(configs)

	if opts.EnableDrop {
		config.EnableDrop = opts.EnableDrop
	}

	options := sqldef.Options{
		DesiredDDLs: desiredDDLs,
		DryRun:      opts.DryRun,
		Export:      opts.Export,
		EnableDrop:  opts.EnableDrop,
		BeforeApply: opts.BeforeApply,
		Config:      config,
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

	dbConfig := database.Config{
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
	if _, err := os.Stat(dbConfig.Host); !os.IsNotExist(err) {
		dbConfig.Socket = dbConfig.Host
	}
	return dbConfig, &options
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

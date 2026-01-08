package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"syscall"

	"github.com/jessevdk/go-flags"
	"github.com/sqldef/sqldef/v3"
	"github.com/sqldef/sqldef/v3/database"
	"github.com/sqldef/sqldef/v3/database/file"
	"github.com/sqldef/sqldef/v3/database/mssql"
	"github.com/sqldef/sqldef/v3/schema"
	"github.com/sqldef/sqldef/v3/util"
	"golang.org/x/term"
)

// Return parsed options and schema filename
// TODO: Support `sqldef schema.sql -opt val...`
func parseOptions(args []string) (database.Config, *sqldef.Options) {
	// MSSQL default: legacy_ignore_quotes is true (legacy mode)
	defaultConfig := database.GeneratorConfig{LegacyIgnoreQuotes: true}
	configs := []database.GeneratorConfig{defaultConfig}

	var opts struct {
		User       string   `short:"U" long:"user" description:"MSSQL user name" value-name:"USERNAME" default:"sa"`
		Password   string   `short:"P" long:"password" description:"MSSQL user password, overridden by $MSSQL_PWD" value-name:"PASSWORD"`
		Host       string   `short:"h" long:"host" description:"Host to connect to the MSSQL server" value-name:"HOSTNAME" default:"127.0.0.1"`
		Port       uint     `short:"p" long:"port" description:"Port used for the connection" value-name:"PORT" default:"1433"`
		Trusted    bool     `short:"E" long:"trusted-connection" description:"Use Windows authentication"`
		Instance   string   `long:"instance" description:"Instance name" value-name:"INSTANCE"`
		TrustCert  bool     `long:"trust-server-cert" description:"Trust server certificate"`
		Prompt     bool     `long:"password-prompt" description:"Force MSSQL user password prompt"`
		File       []string `long:"file" description:"Read desired SQL from the file, rather than stdin" value-name:"FILENAME" default:"-"`
		DryRun     bool     `long:"dry-run" description:"Don't run DDLs but just show them"`
		Apply      bool     `long:"apply" description:"Apply DDLs to the database (default, but will require this flag in future versions)"`
		Export     bool     `long:"export" description:"Just dump the current schema to stdout"`
		EnableDrop bool     `long:"enable-drop" description:"Enable destructive changes such as DROP for TABLE, SCHEMA, ROLE, USER, FUNCTION, PROCEDURE, TRIGGER, VIEW, INDEX, SEQUENCE, TYPE"`

		// Custom handlers for config flags to preserve order
		Config       func(string) `long:"config" description:"YAML configuration file (can be specified multiple times)" value-name:"PATH"`
		ConfigInline func(string) `long:"config-inline" description:"YAML configuration as inline string (can be specified multiple times)" value-name:"YAML"`

		Help    bool `long:"help" description:"Show this help"`
		Version bool `long:"version" description:"Show version information"`
	}

	opts.Config = func(path string) {
		configs = append(configs, database.ParseGeneratorConfig(path, defaultConfig))
	}
	opts.ConfigInline = func(yaml string) {
		configs = append(configs, database.ParseGeneratorConfigString(yaml, defaultConfig))
	}

	parser := flags.NewParser(&opts, flags.None)
	parser.Usage = `[OPTION]... DATABASE --export
  mssqldef [OPTION]... DATABASE --apply < desired.sql
  mssqldef [OPTION]... DATABASE --dry-run < desired.sql
  mssqldef [OPTION]... current.sql < desired.sql`
	args, err := parser.ParseArgs(args)
	if err != nil {
		log.Fatal(err)
	}

	if opts.Help {
		parser.WriteHelp(os.Stdout)
		fmt.Printf("\nFor more information, see: https://github.com/sqldef/sqldef/blob/v%s/cmd-mssqldef.md\n", sqldef.GetVersion())
		os.Exit(0)
	}

	if opts.Version {
		fmt.Println(sqldef.GetFullVersion())
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
		config.EnableDrop = true
	}

	options := sqldef.Options{
		DesiredDDLs: desiredDDLs,
		DryRun:      opts.DryRun,
		Export:      opts.Export,
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

	password, ok := os.LookupEnv("MSSQL_PWD")
	if !ok {
		password = opts.Password
	}

	if opts.Prompt {
		fmt.Fprint(os.Stderr, "Enter Password: ")
		pass, err := term.ReadPassword(int(syscall.Stdin))
		if err != nil {
			log.Fatal(err)
		}
		fmt.Fprintln(os.Stderr)
		password = string(pass)
	}

	dbConfig := database.Config{
		DbName:            databaseName,
		User:              opts.User,
		Password:          password,
		Host:              opts.Host,
		Port:              int(opts.Port),
		TrustedConnection: opts.Trusted,
		Instance:          opts.Instance,
		TrustServerCert:   opts.TrustCert,
	}
	return dbConfig, &options
}

func main() {
	util.InitSlog()

	config, options := parseOptions(os.Args[1:])

	var db database.Database
	if len(options.CurrentFile) > 0 {
		db = file.NewDatabase(options.CurrentFile)
	} else {
		var err error
		db, err = mssql.NewDatabase(config)
		if err != nil {
			log.Fatal(err)
		}
		defer db.Close()
	}

	sqlParser := mssql.NewParser()
	sqldef.Run(schema.GeneratorModeMssql, db, sqlParser, options)
}

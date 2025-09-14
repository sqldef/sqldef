package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"syscall"

	"github.com/sqldef/sqldef/v2/database/file"
	"github.com/sqldef/sqldef/v2/parser"

	"github.com/jessevdk/go-flags"
	"github.com/sqldef/sqldef/v2"
	"github.com/sqldef/sqldef/v2/database"
	"github.com/sqldef/sqldef/v2/database/mysql"
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
		User                  string   `short:"u" long:"user" description:"MySQL user name" value-name:"user_name" default:"root"`
		Password              string   `short:"p" long:"password" description:"MySQL user password, overridden by $MYSQL_PWD" value-name:"password"`
		Host                  string   `short:"h" long:"host" description:"Host to connect to the MySQL server" value-name:"host_name" default:"127.0.0.1"`
		Port                  uint     `short:"P" long:"port" description:"Port used for the connection" value-name:"port_num" default:"3306"`
		Socket                string   `short:"S" long:"socket" description:"The socket file to use for connection" value-name:"socket"`
		SslMode               string   `long:"ssl-mode" description:"SSL connection mode(PREFERRED,REQUIRED,DISABLED)." value-name:"ssl_mode" default:"PREFERRED"`
		SslCa                 string   `long:"ssl-ca" description:"File that contains list of trusted SSL Certificate Authorities" value-name:"ssl_ca"`
		Prompt                bool     `long:"password-prompt" description:"Force MySQL user password prompt"`
		EnableCleartextPlugin bool     `long:"enable-cleartext-plugin" description:"Enable/disable the clear text authentication plugin"`
		File                  []string `long:"file" description:"Read desired SQL from the file, rather than stdin" value-name:"sql_file" default:"-"`
		DryRun                bool     `long:"dry-run" description:"Don't run DDLs but just show them"`
		Export                bool     `long:"export" description:"Just dump the current schema to stdout"`
		EnableDrop            bool     `long:"enable-drop" description:"Enable destructive changes such as DROP for TABLE, SCHEMA, ROLE, USER, FUNCTION, PROCEDURE, TRIGGER, VIEW, INDEX, SEQUENCE, TYPE"`
		SkipView              bool     `long:"skip-view" description:"Skip managing views (temporary feature, to be removed later)"`
		BeforeApply           string   `long:"before-apply" description:"Execute the given string before applying the regular DDLs"`
		Help                  bool     `long:"help" description:"Show this help"`
		Version               bool     `long:"version" description:"Show this version"`

		// Custom handlers for config flags to preserve order
		Config       func(string) `long:"config" description:"YAML file to specify: target_tables, skip_tables, algorithm, lock (can be specified multiple times)"`
		ConfigInline func(string) `long:"config-inline" description:"YAML object to specify: target_tables, skip_tables, algorithm, lock (can be specified multiple times)"`
	}

	opts.Config = func(path string) {
		configs = append(configs, database.ParseGeneratorConfig(path))
	}
	opts.ConfigInline = func(yaml string) {
		configs = append(configs, database.ParseGeneratorConfigString(yaml))
	}

	parser := flags.NewParser(&opts, flags.None)
	parser.Usage = "[OPTIONS] [database|current.sql] < desired.sql"
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

	switch strings.ToLower(opts.SslMode) {
	case "disabled":
		opts.SslMode = "false"
	case "preferred":
		opts.SslMode = "preferred"
	case "required":
		opts.SslMode = "true"
	case "custom":
		opts.SslMode = "custom"
	default:
		fmt.Printf("Wrong value for ssl-mode is given: %v\n\n", opts.SslMode)
		parser.WriteHelp(os.Stdout)
		os.Exit(1)
	}

	password, ok := os.LookupEnv("MYSQL_PWD")
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
		DbName:                     databaseName,
		User:                       opts.User,
		Password:                   password,
		Host:                       opts.Host,
		Port:                       int(opts.Port),
		Socket:                     opts.Socket,
		MySQLEnableCleartextPlugin: opts.EnableCleartextPlugin,
		SkipView:                   opts.SkipView,
		SslMode:                    opts.SslMode,
		SslCa:                      opts.SslCa,
		DumpConcurrency:            options.Config.DumpConcurrency,
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
		db, err = mysql.NewDatabase(config)
		if err != nil {
			log.Fatal(err)
		}
		defer db.Close()
	}

	sqlParser := database.NewParser(parser.ParserModeMysql)
	sqldef.Run(schema.GeneratorModeMysql, db, sqlParser, options)
}

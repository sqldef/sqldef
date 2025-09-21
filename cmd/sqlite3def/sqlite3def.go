package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/jessevdk/go-flags"
	"github.com/sqldef/sqldef/v2"
	"github.com/sqldef/sqldef/v2/database"
	"github.com/sqldef/sqldef/v2/database/file"
	"github.com/sqldef/sqldef/v2/database/sqlite3"
	"github.com/sqldef/sqldef/v2/parser"
	"github.com/sqldef/sqldef/v2/schema"
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
		File       []string `short:"f" long:"file" description:"Read desired SQL from the file, rather than stdin" value-name:"filename" default:"-"`
		DryRun     bool     `long:"dry-run" description:"Don't run DDLs but just show them"`
		Export     bool     `long:"export" description:"Just dump the current schema to stdout"`
		EnableDrop bool     `long:"enable-drop" description:"Enable destructive changes such as DROP for TABLE, SCHEMA, ROLE, USER, FUNCTION, PROCEDURE, TRIGGER, VIEW, INDEX, SEQUENCE, TYPE"`
		Help       bool     `long:"help" description:"Show this help"`
		Version    bool     `long:"version" description:"Show this version"`

		// Custom handlers for config flags to preserve order
		Config       func(string) `long:"config" description:"YAML configuration file (can be specified multiple times)"`
		ConfigInline func(string) `long:"config-inline" description:"YAML configuration as inline string (can be specified multiple times)"`
	}

	opts.Config = func(path string) {
		configs = append(configs, database.ParseGeneratorConfig(path))
	}
	opts.ConfigInline = func(yaml string) {
		configs = append(configs, database.ParseGeneratorConfigString(yaml))
	}

	parser := flags.NewParser(&opts, flags.None)
	parser.Usage = "[OPTIONS] [FILENAME|current.sql] < desired.sql"
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

	dbConfig := database.Config{
		DbName: databaseName,
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
		db, err = sqlite3.NewDatabase(config)
		if err != nil {
			log.Fatal(err)
		}
		defer db.Close()
	}

	sqlParser := database.NewParser(parser.ParserModeSQLite3)
	sqldef.Run(schema.GeneratorModeSQLite3, db, sqlParser, options)
}

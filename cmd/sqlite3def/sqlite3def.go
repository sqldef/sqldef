package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/jessevdk/go-flags"
	"github.com/k0kubun/sqldef"
	"github.com/k0kubun/sqldef/database"
	"github.com/k0kubun/sqldef/database/file"
	"github.com/k0kubun/sqldef/database/sqlite3"
	"github.com/k0kubun/sqldef/parser"
	"github.com/k0kubun/sqldef/schema"
)

var version string

// Return parsed options and schema filename
// TODO: Support `sqldef schema.sql -opt val...`
func parseOptions(args []string) (database.Config, *sqldef.Options) {
	var opts struct {
		File     []string `short:"f" long:"file" description:"Read schema SQL from the file, rather than stdin" value-name:"filename" default:"-"`
		DryRun   bool     `long:"dry-run" description:"Don't run DDLs but just show them"`
		Export   bool     `long:"export" description:"Just dump the current schema to stdout"`
		SkipDrop bool     `long:"skip-drop" description:"Skip destructive changes such as DROP"`
		Config   string   `long:"config" description:"YAML file to specify: target_tables, skip_tables"`
		Help     bool     `long:"help" description:"Show this help"`
		Version  bool     `long:"version" description:"Show this version"`
	}

	parser := flags.NewParser(&opts, flags.None)
	parser.Usage = "[option...] db_name"
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
		DesiredDDLs: desiredDDLs,
		DryRun:      opts.DryRun,
		Export:      opts.Export,
		SkipDrop:    opts.SkipDrop,
		Config:      database.ParseGeneratorConfig(opts.Config),
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

	config := database.Config{
		DbName: databaseName,
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
		db, err = sqlite3.NewDatabase(config)
		if err != nil {
			log.Fatal(err)
		}
		defer db.Close()
	}

	sqlParser := database.NewParser(parser.ParserModeSQLite3)
	sqldef.Run(schema.GeneratorModeSQLite3, db, sqlParser, options)
}

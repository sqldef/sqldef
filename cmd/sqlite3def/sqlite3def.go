package main

import (
	"fmt"
	"log"
	"os"

	"github.com/jessevdk/go-flags"
	"github.com/k0kubun/sqldef"
	"github.com/k0kubun/sqldef/adapter"
	"github.com/k0kubun/sqldef/adapter/sqlite3"
	"github.com/k0kubun/sqldef/schema"
)

var version string

// Return parsed options and schema filename
// TODO: Support `sqldef schema.sql -opt val...`
func parseOptions(args []string) (adapter.Config, *sqldef.Options) {
	var opts struct {
		File     string `short:"f" long:"file" description:"Read schema SQL from the file, rather than stdin" value-name:"filename" default:"-"`
		DryRun   bool   `long:"dry-run" description:"Don't run DDLs but just show them"`
		Export   bool   `long:"export" description:"Just dump the current schema to stdout"`
		SkipDrop bool   `long:"skip-drop" description:"Skip destructive changes such as DROP"`
		Help     bool   `long:"help" description:"Show this help"`
		Version  bool   `long:"version" description:"Show this version"`
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

	if len(args) == 0 {
		fmt.Print("No database is specified!\n\n")
		parser.WriteHelp(os.Stdout)
		os.Exit(1)
	} else if len(args) > 1 {
		fmt.Printf("Multiple databases are given: %v\n\n", args)
		parser.WriteHelp(os.Stdout)
		os.Exit(1)
	}
	database := args[0]

	options := sqldef.Options{
		SqlFile:  opts.File,
		DryRun:   opts.DryRun,
		Export:   opts.Export,
		SkipDrop: opts.SkipDrop,
	}

	config := adapter.Config{
		DbName: database,
	}
	if _, err := os.Stat(config.Host); !os.IsNotExist(err) {
		config.Socket = config.Host
	}
	return config, &options
}

func main() {
	config, options := parseOptions(os.Args[1:])

	database, err := sqlite3.NewDatabase(config)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()

	sqldef.Run(schema.GeneratorModeSQLite3, database, options)
}

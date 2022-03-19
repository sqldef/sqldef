package main

import (
	"fmt"
	"log"
	"os"
	"syscall"

	"github.com/jessevdk/go-flags"
	"github.com/k0kubun/sqldef"
	"github.com/k0kubun/sqldef/adapter"
	"github.com/k0kubun/sqldef/adapter/file"
	"github.com/k0kubun/sqldef/adapter/mssql"
	"github.com/k0kubun/sqldef/schema"
	"golang.org/x/term"
)

var version string

// Return parsed options and schema filename
// TODO: Support `sqldef schema.sql -opt val...`
func parseOptions(args []string) (adapter.Config, *sqldef.Options) {
	var opts struct {
		User       string   `short:"U" long:"user" description:"MSSQL user name" value-name:"user_name" default:"sa"`
		Password   string   `short:"P" long:"password" description:"MSSQL user password, overridden by $MSSQL_PWD" value-name:"password"`
		Host       string   `short:"h" long:"host" description:"Host to connect to the MSSQL server" value-name:"host_name" default:"127.0.0.1"`
		Port       uint     `short:"p" long:"port" description:"Port used for the connection" value-name:"port_num" default:"1433"`
		Prompt     bool     `long:"password-prompt" description:"Force MSSQL user password prompt"`
		File       []string `long:"file" description:"Read schema SQL from the file, rather than stdin" value-name:"sql_file" default:"-"`
		DryRun     bool     `long:"dry-run" description:"Don't run DDLs but just show them"`
		Export     bool     `long:"export" description:"Just dump the current schema to stdout"`
		SkipDrop   bool     `long:"skip-drop" description:"Skip destructive changes such as DROP"`
		TargetFile string   `long:"target-file" description:"Manage the target name (Table, View, Type, Trigger)"`
		Help       bool     `long:"help" description:"Show this help"`
		Version    bool     `long:"version" description:"Show this version"`
	}

	parser := flags.NewParser(&opts, flags.None)
	parser.Usage = "[options] db_name"
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

	desiredFile, currentFile := sqldef.ParseFiles(opts.File)
	options := sqldef.Options{
		DesiredFile: desiredFile,
		CurrentFile: currentFile,
		DryRun:      opts.DryRun,
		Export:      opts.Export,
		SkipDrop:    opts.SkipDrop,
		Targets:     sqldef.ParseTargets(opts.TargetFile),
	}

	database := ""
	if len(currentFile) == 0 {
		if len(args) == 0 {
			fmt.Print("No database is specified!\n\n")
			parser.WriteHelp(os.Stdout)
			os.Exit(1)
		} else if len(args) > 1 {
			fmt.Printf("Multiple databases are given: %v\n\n", args)
			parser.WriteHelp(os.Stdout)
			os.Exit(1)
		}
		database = args[0]
	}

	password, ok := os.LookupEnv("MSSQL_PWD")
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

	config := adapter.Config{
		DbName:   database,
		User:     opts.User,
		Password: password,
		Host:     opts.Host,
		Port:     int(opts.Port),
	}
	return config, &options
}

func main() {
	config, options := parseOptions(os.Args[1:])

	var database adapter.Database
	if len(options.CurrentFile) > 0 {
		database = file.NewDatabase(options.CurrentFile)
	} else {
		var err error
		database, err = mssql.NewDatabase(config)
		if err != nil {
			log.Fatal(err)
		}
		defer database.Close()
	}

	sqldef.Run(schema.GeneratorModeMssql, database, options)
}

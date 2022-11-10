package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"syscall"

	"github.com/k0kubun/sqldef/database/file"
	"github.com/k0kubun/sqldef/parser"

	"github.com/jessevdk/go-flags"
	"github.com/k0kubun/sqldef"
	"github.com/k0kubun/sqldef/database"
	"github.com/k0kubun/sqldef/database/mysql"
	"github.com/k0kubun/sqldef/schema"
	"golang.org/x/term"
)

var version string

// Return parsed options and schema filename
// TODO: Support `sqldef schema.sql -opt val...`
func parseOptions(args []string) (database.Config, *sqldef.Options) {
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
		File                  []string `long:"file" description:"Read schema SQL from the file, rather than stdin" value-name:"sql_file" default:"-"`
		DryRun                bool     `long:"dry-run" description:"Don't run DDLs but just show them"`
		Export                bool     `long:"export" description:"Just dump the current schema to stdout"`
		SkipDrop              bool     `long:"skip-drop" description:"Skip destructive changes such as DROP"`
		SkipView              bool     `long:"skip-view" description:"Skip managing views (temporary feature, to be removed later)"`
		BeforeApply           string   `long:"before-apply" description:"Execute the given string before applying the regular DDLs"`
		Config                string   `long:"config" description:"YAML file to specify: target_tables, skip_tables"`
		Help                  bool     `long:"help" description:"Show this help"`
		Version               bool     `long:"version" description:"Show this version"`
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
		BeforeApply: opts.BeforeApply,
		Config:      database.ParseGeneratorConfig(opts.Config),
	}

	databaseName := ""
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

	config := database.Config{
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
		db, err = mysql.NewDatabase(config)
		if err != nil {
			log.Fatal(err)
		}
		defer db.Close()
	}

	sqlParser := database.NewParser(parser.ParserModeMysql)
	sqldef.Run(schema.GeneratorModeMysql, db, sqlParser, options)
}

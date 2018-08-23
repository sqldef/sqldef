package main

import (
	"fmt"
	"os"

	"github.com/urfave/cli"
)

type Options struct {
	sqlFile    string
	dbType     string
	dbUser     string
	dbPassword string
	dbHost     string
	dbPort     int
	dryRun     bool
	export     bool
}

// Return parsed options and schema filename
// TODO: Support `sqldef schema.sql -opt val...`
func parseOptions(args []string) (string, *Options) {
	app := cli.NewApp()
	app.HelpName = "sqldef"
	app.Version = "0.0.1"

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "f, file",
			Value: "schema.sql",
			Usage: "SQL file path to be applied",
		},
		cli.StringFlag{
			Name:  "u, user",
			Value: "root",
			Usage: "Database user",
		},
		cli.StringFlag{
			Name:  "p, password",
			Value: "",
			Usage: "Database password",
		},
		cli.StringFlag{
			Name:  "H, host", // FIXME: -h is used by --help......
			Value: "127.0.0.1",
			Usage: "Database host",
		},
		cli.IntFlag{
			Name:  "P, port",
			Value: 3306,
			Usage: "Database port",
		},
		cli.StringFlag{
			Name:  "type",
			Value: "mysql",
			Usage: "mysql, postgres",
		},
		cli.BoolFlag{
			Name:  "dry-run",
			Usage: "Don't run DDLs but show them",
		},
		cli.BoolFlag{
			Name:  "export",
			Usage: "Just dump the current DDLs to stdout",
		},
	}

	cli.AppHelpTemplate = `USAGE:
   {{if .UsageText}}{{.UsageText}}{{else}}{{.HelpName}} {{if .VisibleFlags}}[OPTIONS]{{end}} [database]{{end}}{{if .VisibleFlags}}

OPTIONS:
   {{range $index, $option := .VisibleFlags}}{{if $index}}
   {{end}}{{$option}}{{end}}{{end}}

`

	var database string
	actionRun := false
	options := Options{}

	app.Action = func(c *cli.Context) error {
		actionRun = true
		options.sqlFile = c.String("file")
		options.dbType = c.String("type")
		options.dbUser = c.String("user")
		options.dbPassword = c.String("password")
		options.dbHost = c.String("host")
		options.dbPort = c.Int("port")
		options.dryRun = c.Bool("dry-run")
		options.export = c.Bool("export")

		if len(c.Args()) == 0 {
			fmt.Println("No database is specified!\n")
			cli.ShowAppHelp(c)
			os.Exit(1)
		} else if len(c.Args()) > 1 {
			fmt.Printf("Multiple arguments are given: %v\n\n", c.Args())
			cli.ShowAppHelp(c)
			os.Exit(1)
		}
		database = c.Args()[0]
		return nil
	}
	app.Run(args)

	if len(database) == 0 && !actionRun {
		// Help triggered or "" is specified
		// TODO: Handle -h/--help case properly...
		os.Exit(0)
	}

	return database, &options
}

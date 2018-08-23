package main

import (
	"fmt"
	"os"

	"github.com/k0kubun/sqldef"
	"github.com/urfave/cli"
)

// Return parsed options and schema filename
// TODO: Support `sqldef schema.sql -opt val...`
func parseOptions(args []string) (string, *sqldef.Options) {
	app := cli.NewApp()
	app.HelpName = "psqldef"
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
	options := sqldef.Options{
		DbType: "postgres",
	}

	app.Action = func(c *cli.Context) error {
		actionRun = true
		options.SqlFile = c.String("file")
		options.DbUser = c.String("user")
		options.DbPassword = c.String("password")
		options.DbHost = c.String("host")
		options.DbPort = c.Int("port")
		options.DryRun = c.Bool("dry-run")
		options.Export = c.Bool("export")

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

func main() {
	database, options := parseOptions(os.Args)
	sqldef.Run(database, options)
}

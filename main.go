package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"github.com/k0kubun/schemasql/schema"
	"github.com/urfave/cli"
)

type Options struct {
	dbType string
}

// Return parsed options and schema filename
// TODO: Support `schemasql schema.sql -opt val...`
func parseOptions(args []string) (string, *Options) {
	app := cli.NewApp()
	app.HelpName = "schemasql"
	app.Version = "0.0.1"

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "type",
			Value: "mysql",
			Usage: "Type of database (mysql, postgresql)",
		},
	}

	cli.AppHelpTemplate = `USAGE:
   {{if .UsageText}}{{.UsageText}}{{else}}{{.HelpName}} {{if .VisibleFlags}}[options]{{end}} [sql file]{{end}}{{if .Description}}

DESCRIPTION:
   {{.Description}}{{end}}{{if len .Authors}}

AUTHOR{{with $length := len .Authors}}{{if ne 1 $length}}S{{end}}{{end}}:
   {{range $index, $author := .Authors}}{{if $index}}
   {{end}}{{$author}}{{end}}{{end}}{{if .VisibleFlags}}

OPTIONS:
   {{range $index, $option := .VisibleFlags}}{{if $index}}
   {{end}}{{$option}}{{end}}{{end}}

`

	var file string
	options := Options{}

	app.Action = func(c *cli.Context) error {
		options.dbType = c.String("type")

		if len(c.Args()) == 0 {
			fmt.Println("No schema file argument is given!\n")
			cli.ShowAppHelp(c)
			os.Exit(1)
		} else if len(c.Args()) > 1 {
			fmt.Printf("Multiple schema file arguments are given: %v\n\n", c.Args())
			cli.ShowAppHelp(c)
			os.Exit(1)
		}
		file = c.Args()[0]
		return nil
	}
	app.Run(args)

	return file, &options
}

func main() {
	filename, _ := parseOptions(os.Args)

	sql, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Fatal(err)
	}

	schema.ParseDDLs(string(sql))
	fmt.Println("success!")
}

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"github.com/k0kubun/schemasql/schema"
)

type Options struct {
	dbType string
}

// Return parsed options and only one filename
func parseOptions(args []string) (string, *Options) {
	flags := flag.NewFlagSet(args[0], flag.ExitOnError)
	var (
		dbType = flags.String("type", "mysql", "Type of database")
	)

	flags.Parse(args[1:])
	files := flags.Args()

	if len(files) == 0 {
		fmt.Println("No schema file argument is given!\n")
		flags.Usage()
		os.Exit(1)
	} else if len(files) > 1 {
		fmt.Println("Multiple schema file arguments are given!\n")
		flags.Usage()
		os.Exit(1)
	}

	return files[0], &Options{
		dbType: *dbType,
	}
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

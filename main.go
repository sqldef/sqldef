package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"github.com/k0kubun/schemasql/schema"
)

func main() {
	database, options := parseOptions(os.Args)

	sql, err := ioutil.ReadFile(options.sqlFile)
	if err != nil {
		log.Fatal(err)
	}

	schema.ParseDDLs(string(sql))

	_ = database
	fmt.Println("success!")
}

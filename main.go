package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"github.com/k0kubun/schemasql/driver"
	"github.com/k0kubun/schemasql/schema"
)

func main() {
	database, options := parseOptions(os.Args)

	sql, err := ioutil.ReadFile(options.sqlFile)
	if err != nil {
		log.Fatal(err)
	}

	ddls, err := schema.ParseDDLs(string(sql))
	if err != nil {
		log.Fatal(err)
	}

	db := driver.NewDatabase(driver.Config{
		DbType:   options.dbType,
		Database: database,
	})
	tables := db.TableNames()

	ddls = schema.GenerateIdempotentDDLs(ddls, tables)
	fmt.Println("success!")
}

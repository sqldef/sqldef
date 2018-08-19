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

	db, err := driver.NewDatabase(driver.Config{
		DbType: options.dbType,
		DbName: database,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	tables, err := db.TableNames()
	if err != nil {
		log.Fatal(err)
	}

	ddls = schema.GenerateIdempotentDDLs(ddls, tables)
	fmt.Println("success!")
}

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

	ddls, err := schema.GenerateIdempotentDDLs(string(sql), tables)
	if err != nil {
		log.Fatal(err)
	}
	if len(ddls) == 0 {
		fmt.Println("Nothing is modified")
		return
	}

	err = db.RunDDLs(ddls)
	if err != nil {
		log.Fatal(err)
	}
}

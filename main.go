package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"github.com/k0kubun/sqldef/driver"
	"github.com/k0kubun/sqldef/schema"
)

func main() {
	database, options := parseOptions(os.Args)

	sql, err := ioutil.ReadFile(options.sqlFile)
	if err != nil {
		log.Fatal(err)
	}
	desiredDDLs := string(sql)

	db, err := driver.NewDatabase(driver.Config{
		DbType:   options.dbType,
		DbName:   database,
		User:     options.dbUser,
		Password: options.dbPassword,
		Host:     options.dbHost,
		Port:     options.dbPort,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	currentDDLs, err := db.DumpDDLs()
	if err != nil {
		log.Fatal(err)
	}

	if options.export {
		fmt.Printf("%s;\n", currentDDLs)
		return
	}

	ddls, err := schema.GenerateIdempotentDDLs(desiredDDLs, currentDDLs)
	if err != nil {
		log.Fatal(err)
	}
	if len(ddls) == 0 {
		fmt.Println("Nothing is modified")
		return
	}

	if options.dryRun {
		showDDLs(ddls)
		return
	}

	err = db.RunDDLs(ddls)
	if err != nil {
		log.Fatal(err)
	}
}

func showDDLs(ddls []string) {
	fmt.Println("--- dry run ---")
	for _, ddl := range ddls {
		fmt.Printf("Run: '%s'\n", ddl)
	}
}

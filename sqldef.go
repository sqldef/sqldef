package sqldef

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"github.com/k0kubun/sqldef/driver"
	"github.com/k0kubun/sqldef/schema"
)

type Options struct {
	SqlFile    string
	DbType     string
	DbUser     string
	DbPassword string
	DbHost     string
	DbPort     int
	DryRun     bool
	Export     bool
}

// Main function shared by `mysqldef` and `psqldef`
func Run(database string, options *Options) {
	sql, err := readFile(options.SqlFile)
	if err != nil {
		log.Fatalf("Failed to read '%s': %s", options.SqlFile, err)
	}
	desiredDDLs := string(sql)

	db, err := driver.NewDatabase(driver.Config{
		DbType:   options.DbType,
		DbName:   database,
		User:     options.DbUser,
		Password: options.DbPassword,
		Host:     options.DbHost,
		Port:     options.DbPort,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	currentDDLs, err := db.DumpDDLs()
	if err != nil {
		log.Fatal(err)
	}

	if options.Export {
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

	if options.DryRun {
		showDDLs(ddls)
		return
	}

	err = db.RunDDLs(ddls)
	if err != nil {
		log.Fatal(err)
	}
}

func readFile(filepath string) (string, error) {
	var content string
	var err error
	if filepath == "-" {
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) != 0 {
			return "", fmt.Errorf("stdin is not piped")
		}

		var buffer bytes.Buffer
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			buffer.WriteString(scanner.Text())
		}
		content = buffer.String()
	} else {
		var buf []byte
		buf, err = ioutil.ReadFile(filepath)
		content = string(buf)
	}

	if err != nil {
		return "", err
	}
	return content, nil
}

func showDDLs(ddls []string) {
	fmt.Println("--- dry run ---")
	for _, ddl := range ddls {
		fmt.Printf("Run: '%s;'\n", ddl)
	}
}

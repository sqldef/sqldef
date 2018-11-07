package sqldef

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"github.com/k0kubun/sqldef/adapter"
	"github.com/k0kubun/sqldef/schema"
)

type Options struct {
	SqlFile string
	DryRun  bool
	Export  bool
}

// Main function shared by `mysqldef` and `psqldef`
func Run(generatorMode schema.GeneratorMode, db adapter.Database, options *Options) {
	currentDDLs, err := adapter.DumpDDLs(db)
	if err != nil {
		log.Fatal(fmt.Sprintf("Error on DumpDDLs: %s", err))
	}

	if options.Export {
		if currentDDLs == "" {
			fmt.Printf("-- No table exists --\n")
		} else {
			fmt.Printf("%s;\n", currentDDLs)
		}
		return
	}

	sql, err := readFile(options.SqlFile)
	if err != nil {
		log.Fatalf("Failed to read '%s': %s", options.SqlFile, err)
	}
	desiredDDLs := string(sql)

	ddls, err := schema.GenerateIdempotentDDLs(generatorMode, desiredDDLs, currentDDLs)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(ddls) == 0 {
		fmt.Println("-- Nothing is modified --")
		return
	}

	if options.DryRun {
		showDDLs(ddls)
		return
	}

	err = adapter.RunDDLs(db, ddls)
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
	fmt.Println("-- dry run --")
	for _, ddl := range ddls {
		fmt.Printf("%s;\n", ddl)
	}
}

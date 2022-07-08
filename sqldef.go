package sqldef

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"github.com/k0kubun/sqldef/database"
	"github.com/k0kubun/sqldef/schema"
)

type Options struct {
	DesiredFile string
	CurrentFile string
	DryRun      bool
	Export      bool
	SkipDrop    bool
	SkipTables  []string
	BeforeApply string
	Config      database.GeneratorConfig
}

// Main function shared by all commands
func Run(generatorMode schema.GeneratorMode, db database.Database, sqlParser database.Parser, options *Options) {
	currentDDLs, err := db.DumpDDLs()
	if err != nil {
		log.Fatalf("Error on DumpDDLs: %s", err)
	}

	if options.Export {
		if currentDDLs == "" {
			fmt.Printf("-- No table exists --\n")
		} else {
			ddls, err := schema.ParseDDLs(generatorMode, sqlParser, currentDDLs)
			if err != nil {
				log.Fatal(err)
			}
			ddls = schema.FilterTables(ddls, options.SkipTables, options.Config)
			for i, ddl := range ddls {
				if i > 0 {
					fmt.Println()
				}
				fmt.Printf("%s;\n", ddl.Statement())
			}
		}
		return
	}

	sql, err := ReadFile(options.DesiredFile)
	if err != nil {
		log.Fatalf("Failed to read '%s': %s", options.DesiredFile, err)
	}
	desiredDDLs := sql

	ddls, err := schema.GenerateIdempotentDDLs(generatorMode, sqlParser, desiredDDLs, currentDDLs, options.SkipTables, options.Config)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(ddls) == 0 {
		fmt.Println("-- Nothing is modified --")
		return
	}

	if options.DryRun || len(options.CurrentFile) > 0 {
		showDDLs(ddls, options.SkipDrop, options.BeforeApply)
		return
	}

	err = database.RunDDLs(db, ddls, options.SkipDrop, options.BeforeApply)
	if err != nil {
		log.Fatal(err)
	}
}

// TODO: Warn if both the second --file and database options are specified
func ParseFiles(files []string) (string, string) {
	if len(files) == 0 {
		panic("ParseFiles got empty files") // assume default:"-"
	}

	desiredFile := files[0]
	currentFile := ""
	if len(files) == 2 {
		desiredFile = files[1]
		currentFile = files[0]
	} else if len(files) > 2 {
		fmt.Printf("Expected only one or two --file options, but got: %v\n", files)
		os.Exit(1)
	}
	return desiredFile, currentFile
}

func ReadFile(filepath string) (string, error) {
	var err error
	var buf []byte

	if filepath == "-" {
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) != 0 {
			return "", fmt.Errorf("stdin is not piped")
		}

		buf, err = ioutil.ReadAll(os.Stdin)
	} else {
		buf, err = ioutil.ReadFile(filepath)
	}

	if err != nil {
		return "", err
	}
	return string(buf), nil
}

func showDDLs(ddls []string, skipDrop bool, beforeApply string) {
	fmt.Println("-- dry run --")
	if len(beforeApply) > 0 {
		fmt.Println(beforeApply)
	}
	for _, ddl := range ddls {
		if skipDrop && strings.Contains(ddl, "DROP") {
			fmt.Printf("-- Skipped: %s;\n", ddl)
			continue
		}
		fmt.Printf("%s;\n", ddl)
	}
}

func ParseSkipTables(skipFile string) []string {
	skipTables := []string{}
	if raw, err := ReadFile(skipFile); err == nil {
		trimmedRaw := strings.TrimSpace(raw)
		if trimmedRaw == "" {
			return []string{}
		}
		skipTables = strings.Split(strings.Trim(trimmedRaw, "\n"), "\n")
	}

	return skipTables
}

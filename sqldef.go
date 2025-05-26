package sqldef

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/sqldef/sqldef/database"
	"github.com/sqldef/sqldef/schema"
)

type Options struct {
	DesiredDDLs string
	CurrentFile string
	DryRun      bool
	Export      bool
	EnableDrop  bool
	BeforeApply string
	Config      database.GeneratorConfig
}

// Main function shared by all commands
func Run(generatorMode schema.GeneratorMode, db database.Database, sqlParser database.Parser, options *Options) {
	currentDDLs, err := db.DumpDDLs()
	if err != nil {
		log.Fatalf("Error on DumpDDLs: %s", err)
	}

	defaultSchema := db.GetDefaultSchema()

	var ddlSuffix string
	if generatorMode == schema.GeneratorModeMssql {
		ddlSuffix = "GO\n"
	} else {
		ddlSuffix = ""
	}

	if options.Export {
		if currentDDLs == "" {
			fmt.Printf("-- No table exists --\n")
		} else {
			ddls, err := schema.ParseDDLs(generatorMode, sqlParser, currentDDLs, defaultSchema)
			if err != nil {
				log.Fatal(err)
			}
			ddls = schema.FilterTables(ddls, options.Config)
			ddls = schema.FilterViews(ddls, options.Config)
			for i, ddl := range ddls {
				if i > 0 {
					fmt.Println()
				}
				fmt.Printf("%s;\n", ddl.Statement())
				fmt.Print(ddlSuffix)
			}
		}
		return
	}

	ddls, err := schema.GenerateIdempotentDDLs(generatorMode, sqlParser, options.DesiredDDLs, currentDDLs, options.Config, defaultSchema)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(ddls) == 0 {
		fmt.Println("-- Nothing is modified --")
		return
	}

	if options.DryRun || len(options.CurrentFile) > 0 {
		showDDLs(ddls, options.EnableDrop, options.BeforeApply, ddlSuffix)
		return
	}

	err = database.RunDDLs(db, ddls, options.EnableDrop, options.BeforeApply, ddlSuffix)
	if err != nil {
		log.Fatal(err)
	}
}

func ParseFiles(files []string) []string {
	if len(files) == 0 {
		panic("ParseFiles got empty files") // assume default:"-"
	}

	result := make([]string, 0, len(files))
	for _, f := range files {
		result = append(result, strings.Split(f, ",")...)
	}
	for i, r := range result {
		result[i] = strings.TrimSpace(r)
	}
	return result
}

func ReadFiles(filepaths []string) (string, error) {
	var result strings.Builder
	for _, filepath := range filepaths {
		f, err := ReadFile(filepath)
		if err != nil {
			return "", err
		}
		_, err = result.WriteString(f)
		if err != nil {
			return "", err
		}
	}
	return result.String(), nil
}

func ReadFile(filepath string) (string, error) {
	var err error
	var buf []byte

	if filepath == "-" {
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) != 0 {
			return "", fmt.Errorf("stdin is not piped")
		}

		buf, err = io.ReadAll(os.Stdin)
	} else {
		buf, err = os.ReadFile(filepath)
	}

	if err != nil {
		return "", err
	}
	return string(buf), nil
}

func showDDLs(ddls []string, enableDropTable bool, beforeApply string, ddlSuffix string) {
	fmt.Println("-- dry run --")
	if len(beforeApply) > 0 {
		fmt.Println(beforeApply)
	}
	for _, ddl := range ddls {
		if !enableDropTable && strings.Contains(ddl, "DROP TABLE") {
			fmt.Printf("-- Skipped: %s;\n", ddl)
			continue
		}
		fmt.Printf("%s;\n", ddl)
		fmt.Print(ddlSuffix)
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

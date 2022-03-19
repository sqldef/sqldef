package sqldef

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"github.com/k0kubun/sqldef/adapter"
	"github.com/k0kubun/sqldef/schema"
)

type Options struct {
	DesiredFile string
	CurrentFile string
	DryRun      bool
	Export      bool
	SkipDrop    bool
	BeforeApply string
	Targets     []string
}

// Main function shared by `mysqldef` and `psqldef`
func Run(generatorMode schema.GeneratorMode, db adapter.Database, options *Options) {
	currentDDLs, err := adapter.DumpDDLs(db)
	if err != nil {
		log.Fatal(fmt.Sprintf("Error on DumpDDLs: %s", err))
	}
	currentDDLs = filterTargets(generatorMode, currentDDLs, options.Targets)

	if options.Export {
		if currentDDLs == "" {
			fmt.Printf("-- No table exists --\n")
		} else {
			fmt.Printf("%s\n", currentDDLs)
		}
		return
	}

	sql, err := ReadFile(options.DesiredFile)
	if err != nil {
		log.Fatalf("Failed to read '%s': %s", options.DesiredFile, err)
	}
	desiredDDLs := sql

	ddls, err := schema.GenerateIdempotentTargetedDDLs(generatorMode, desiredDDLs, currentDDLs, options.Targets)
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

	err = adapter.RunDDLs(db, ddls, options.SkipDrop, options.BeforeApply)
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

func filterTargets(mode schema.GeneratorMode, currentDDLs string, targets []string) string {

	if len(targets) <= 0 {
		return currentDDLs
	}

	ddls, err := schema.ParseDDLs(mode, currentDDLs)
	if err != nil {
		return currentDDLs
	}

	filtered := []string{}
	for _, ddl := range ddls {
		if containsString(targets, ddl.Name()) {
			filtered = append(filtered, ddl.Statement()+";")
		}
	}

	return strings.Join(filtered, "\n\n")
}

func containsString(strs []string, str string) bool {
	for _, s := range strs {
		if s == str {
			return true
		}
	}
	return false
}

func ParseTargets(targetFile string) []string {
	targets := []string{}
	if raw, err := ReadFile(targetFile); err == nil {
		trimmedRaw := strings.TrimSpace(raw)
		if trimmedRaw == "" {
			return []string{}
		}
		targets = strings.Split(strings.Trim(trimmedRaw, "\n"), "\n")
	}

	return targets
}

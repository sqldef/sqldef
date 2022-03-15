package sqldef

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"regexp"
	"strings"

	"github.com/k0kubun/sqldef/adapter"
	"github.com/k0kubun/sqldef/schema"
)

type Options struct {
	DesiredFile          string
	CurrentFile          string
	DryRun               bool
	Export               bool
	SkipDrop             bool
	BeforeApply          string
	IgnorePartitionRange bool
	Targets              []string
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
	currentDDLs = filterDDLs(currentDDLs, options)

	sql, err := ReadFile(options.DesiredFile)
	if err != nil {
		log.Fatalf("Failed to read '%s': %s", options.DesiredFile, err)
	}
	desiredDDLs := filterDDLs(sql, options)

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

func filterDDLs(sql string, options *Options) string {
	if options.IgnorePartitionRange {
		sql = filterPartitionRange(sql)
	}
	return sql
}

// For MySQL
// Filter specific code for environment-dependent `PARTITION BY RANGE`
func filterPartitionRange(sql string) string {
	return regexp.MustCompile(`\n\/\*![0-9]* PARTITION BY RANGE[\s\S]*?\*\/`).ReplaceAllString(sql, "")
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

func MargeTargets(targets string, targetFile string) []string {
	result := []string{}

	t1 := strings.Split(strings.TrimSpace(targets), ",")
	if len(t1) <= 0 || t1[0] != "" {
		result = append(result, t1...)
	}

	if raw, err := ReadFile(targetFile); err == nil {
		t2 := strings.Split(strings.Trim(strings.TrimSpace(raw), "\n"), "\n")
		if len(t2) <= 0 || t2[0] != "" {
			result = append(result, t2...)
		}
	}

	return result
}

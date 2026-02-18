package sqldef

import (
	_ "embed"
	"fmt"
	"io"
	"log"
	"os"
	"runtime/debug"
	"strings"

	"github.com/sqldef/sqldef/v3/database"
	"github.com/sqldef/sqldef/v3/schema"
	"github.com/sqldef/sqldef/v3/util"
)

//go:embed VERSION
var version string

// GetVersion returns the version of sqldef read from VERSION file.
func GetVersion() string {
	return strings.TrimSpace(version)
}

// GetRevision returns the git revision of sqldef.
// It is automatically populated from Go's embedded VCS info.
// Returns empty string if vcs.revision is not available (e.g., when built without .git directory).
func GetRevision() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && len(setting.Value) >= 7 {
				return setting.Value[:7]
			}
		}
	}
	return ""
}

// GetFullVersion returns the version string for --version output.
// Returns "X.Y.Z (abc1234)" if revision is available, otherwise just "X.Y.Z".
func GetFullVersion() string {
	if rev := GetRevision(); rev != "" {
		return fmt.Sprintf("%s (%s)", GetVersion(), rev)
	}
	return GetVersion()
}

type Options struct {
	DesiredDDLs string
	CurrentFile string
	DryRun      bool
	Export      bool
	BeforeApply string
	Config      database.GeneratorConfig
}

// Main function shared by all commands
func Run(generatorMode schema.GeneratorMode, db database.Database, sqlParser database.Parser, options *Options) {
	// Set the generator config on the database for privilege filtering
	// Note: MySQL will populate MysqlLowerCaseTableNames from the server
	db.SetGeneratorConfig(options.Config)
	options.Config = db.GetGeneratorConfig()

	defaultSchema := db.GetDefaultSchema()
	desiredDDLs, err := schema.ParseDDLs(generatorMode, sqlParser, options.DesiredDDLs, defaultSchema)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	scope := schema.DesiredScope(desiredDDLs, options.Export, options.Config.EnableDrop)
	db.SetMigrationScope(scope)

	currentDDLs, err := db.ExportDDLs()
	if err != nil {
		log.Fatalf("Error on ExportDDLs: %s", err)
	}

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
			ddls = schema.FilterPrivileges(ddls, options.Config)
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
		dryRunDB, err := database.NewDryRunDatabase(db)
		if err != nil {
			log.Fatal(err)
		}
		defer dryRunDB.Close()
		db = dryRunDB
	}

	err = database.RunDDLs(db, ddls, options.BeforeApply, ddlSuffix, database.StdoutLogger{})
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

	return util.TransformSlice(result, func(r string) string {
		return strings.TrimSpace(r)
	})
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

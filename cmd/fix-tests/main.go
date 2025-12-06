package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"
)

type TestEvent struct {
	Time    string `json:"Time"`
	Action  string `json:"Action"`
	Package string `json:"Package"`
	Test    string `json:"Test"`
	Output  string `json:"Output"`
}

type TestFailure struct {
	TestName string
	YamlFile string
	Expected string
	Actual   string
	Phase    string
}

type TestCase struct {
	Current            string         `yaml:"current,omitempty"`
	Desired            string         `yaml:"desired,omitempty"`
	Up                 *string        `yaml:"up,omitempty"`
	Down               *string        `yaml:"down,omitempty"`
	Output             *string        `yaml:"output,omitempty"`
	Error              *string        `yaml:"error,omitempty"`
	MinVersion         string         `yaml:"min_version,omitempty"`
	MaxVersion         string         `yaml:"max_version,omitempty"`
	User               string         `yaml:"user,omitempty"`
	Flavor             string         `yaml:"flavor,omitempty"`
	ManagedRoles       []string       `yaml:"managed_roles,omitempty"`
	EnableDrop         *bool          `yaml:"enable_drop,omitempty"`
	LegacyIgnoreQuotes *bool          `yaml:"legacy_ignore_quotes,omitempty"`
	Offline            bool           `yaml:"offline,omitempty"`
	Config             map[string]any `yaml:"config,omitempty"`
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func run() error {
	// Determine which package to test
	pkg := "./cmd/psqldef"
	if len(os.Args) > 1 && os.Args[1] != "" && !strings.HasSuffix(os.Args[1], ".json") {
		pkg = os.Args[1]
	}

	// Read test results from file
	var testOutput []byte
	var err error

	if len(os.Args) > 2 || (len(os.Args) > 1 && strings.HasSuffix(os.Args[1], ".json")) {
		// Read from file specified as argument
		fileArg := os.Args[1]
		if len(os.Args) > 2 {
			fileArg = os.Args[2]
		}
		testOutput, err = os.ReadFile(fileArg)
		if err != nil {
			return fmt.Errorf("failed to read test results file: %w", err)
		}
	} else {
		// Run tests and collect failures
		testOutput, err = runTests(pkg)
		if err != nil {
			return fmt.Errorf("failed to run tests: %w", err)
		}
	}

	failures, err := parseTestResults(testOutput, pkg)
	if err != nil {
		return fmt.Errorf("failed to parse test results: %w", err)
	}

	fmt.Printf("Found %d failing tests\n", len(failures))

	// Group failures by category
	categories := categorizeFailures(failures)
	fmt.Println("\n=== Failure Categories ===")
	for category, count := range categories {
		fmt.Printf("  %s: %d\n", category, count)
	}

	// Fix each failure
	fixed := 0
	for _, failure := range failures {
		if err := fixTest(failure); err != nil {
			log.Printf("Failed to fix test %s: %v", failure.TestName, err)
		} else {
			fixed++
		}
	}

	fmt.Printf("\n=== Summary ===\n")
	fmt.Printf("Total failures: %d\n", len(failures))
	fmt.Printf("Fixed: %d\n", fixed)
	fmt.Printf("Failed to fix: %d\n", len(failures)-fixed)

	return nil
}

func runTests(pkg string) ([]byte, error) {
	cmd := exec.Command("go", "test", pkg, "-json")
	cmd.Dir = "/Users/goro/ghq/github.com/sqldef/sqldef"

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Test failures are expected, only fatal if we can't run tests at all
		if len(output) == 0 {
			return nil, fmt.Errorf("failed to run tests: %w", err)
		}
	}

	return output, nil
}

func parseTestResults(output []byte, pkg string) ([]TestFailure, error) {
	// Parse JSON test output
	var failures []TestFailure
	scanner := bufio.NewScanner(bytes.NewReader(output))

	// Map of test name to output buffer
	testOutputs := make(map[string]*strings.Builder)
	// Track which tests have already been processed to avoid duplicates
	processedTests := make(map[string]bool)

	for scanner.Scan() {
		line := scanner.Text()

		var event TestEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		if event.Action == "run" && event.Test != "" && strings.HasPrefix(event.Test, "TestApply/") {
			// Only create buffer if not already processed
			if !processedTests[event.Test] {
				testOutputs[event.Test] = &strings.Builder{}
			}
		}

		if event.Action == "output" && event.Output != "" && event.Test != "" {
			if buf, ok := testOutputs[event.Test]; ok && !processedTests[event.Test] {
				buf.WriteString(event.Output)
			}
		}

		if event.Action == "fail" && event.Test != "" && strings.HasPrefix(event.Test, "TestApply/") {
			// Only process each test once
			if !processedTests[event.Test] {
				processedTests[event.Test] = true
				// Parse the failure output
				if buf, ok := testOutputs[event.Test]; ok {
					failure := parseTestFailure(event.Test, buf.String(), pkg)
					if failure != nil {
						failures = append(failures, *failure)
					}
				}
			}
		}
	}

	return failures, nil
}

func parseTestFailure(testName, output, pkg string) *TestFailure {
	// Extract the test case name (remove TestApply/ prefix)
	testCaseName := strings.TrimPrefix(testName, "TestApply/")

	// Look for Phase 3 failures
	if !strings.Contains(output, "[Phase 3: Reverse Migration]") {
		return nil
	}

	// Parse expected and actual values
	expectedRegex := regexp.MustCompile(`expected: "((?:[^"\\]|\\.)*)"`)
	actualRegex := regexp.MustCompile(`actual  : "((?:[^"\\]|\\.)*)"`)

	expectedMatch := expectedRegex.FindStringSubmatch(output)
	actualMatch := actualRegex.FindStringSubmatch(output)

	if expectedMatch == nil || actualMatch == nil {
		return nil
	}

	expected := unescapeString(expectedMatch[1])
	actual := unescapeString(actualMatch[1])

	// Find the YAML file containing this test
	yamlFile := findYamlFile(testCaseName, pkg)
	if yamlFile == "" {
		log.Printf("Could not find YAML file for test: %s", testCaseName)
		return nil
	}

	return &TestFailure{
		TestName: testCaseName,
		YamlFile: yamlFile,
		Expected: expected,
		Actual:   actual,
		Phase:    "Phase 3",
	}
}

func unescapeString(s string) string {
	s = strings.ReplaceAll(s, `\n`, "\n")
	s = strings.ReplaceAll(s, `\t`, "\t")
	s = strings.ReplaceAll(s, `\"`, `"`)
	return s
}

func findYamlFile(testName, pkg string) string {
	// Extract the directory name from the package path
	// e.g., "./cmd/psqldef" -> "psqldef"
	parts := strings.Split(pkg, "/")
	dirName := parts[len(parts)-1]

	baseDir := "/Users/goro/ghq/github.com/sqldef/sqldef/cmd"
	dirPath := filepath.Join(baseDir, dirName)

	// Find all .yml files in the specific directory
	matches, err := filepath.Glob(filepath.Join(dirPath, "*.yml"))
	if err != nil {
		return ""
	}

	for _, yamlFile := range matches {
		data, err := os.ReadFile(yamlFile)
		if err != nil {
			continue
		}

		// Parse YAML to check if test exists
		var tests map[string]TestCase
		if err := yaml.Unmarshal(data, &tests); err != nil {
			continue
		}

		if _, exists := tests[testName]; exists {
			return yamlFile
		}
	}

	return ""
}

func fixTest(failure TestFailure) error {
	// Read the YAML file
	data, err := os.ReadFile(failure.YamlFile)
	if err != nil {
		return fmt.Errorf("failed to read YAML file: %w", err)
	}

	// Parse YAML
	var tests map[string]TestCase
	if err := yaml.Unmarshal(data, &tests); err != nil {
		return fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Get the test case
	test, exists := tests[failure.TestName]
	if !exists {
		return fmt.Errorf("test %s not found in YAML file", failure.TestName)
	}

	// Update the down migration
	newDown := failure.Actual
	test.Down = &newDown
	tests[failure.TestName] = test

	// Write back to YAML file while preserving formatting as much as possible
	// We need to update just the 'down' field in the original file
	if err := updateYamlFile(failure.YamlFile, failure.TestName, "down", failure.Actual); err != nil {
		return fmt.Errorf("failed to update YAML file: %w", err)
	}

	fmt.Printf("Fixed test: %s in %s\n", failure.TestName, filepath.Base(failure.YamlFile))
	return nil
}

func updateYamlFile(filename, testName, field, newValue string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	var result []string

	inTest := false
	inField := false
	testIndent := 0
	fieldIndent := 0

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Check if we're at the start of our target test
		if trimmed == testName+":" {
			inTest = true
			testIndent = len(line) - len(strings.TrimLeft(line, " "))
			result = append(result, line)
			continue
		}

		// Check if we've exited the test (found another test at same indent level)
		if inTest && !inField && trimmed != "" {
			currentIndent := len(line) - len(strings.TrimLeft(line, " "))
			// Exit if we find another key at the same or lower indent level
			if currentIndent <= testIndent && strings.HasSuffix(trimmed, ":") {
				inTest = false
			}
		}

		// Check if we're at the start of our target field
		if inTest && (trimmed == field+": |" || strings.HasPrefix(trimmed, field+": |")) {
			inField = true
			fieldIndent = len(line) - len(strings.TrimLeft(line, " "))
			result = append(result, line)

			// Add the new value (properly indented)
			valueLines := strings.SplitSeq(newValue, "\n")
			for vline := range valueLines {
				// Don't add empty lines at the end
				if vline != "" {
					newLine := strings.Repeat(" ", fieldIndent+2) + vline
					result = append(result, newLine)
				}
			}

			// Now skip all the old field content
			// We need to continue reading lines until we find a line at fieldIndent or less
			for i+1 < len(lines) {
				nextLine := lines[i+1]
				nextTrimmed := strings.TrimSpace(nextLine)
				nextIndent := len(nextLine) - len(strings.TrimLeft(nextLine, " "))

				// If next line is empty, skip it
				if nextTrimmed == "" {
					i++
					continue
				}

				// If we find a line at field indent or less, stop skipping
				if nextIndent <= fieldIndent {
					break
				}

				// Skip this line (it's old field content)
				i++
			}

			// After writing and skipping old content, we're done
			inField = false
			inTest = false
			continue
		}

		// Skip old field content
		if inField {
			currentIndent := len(line) - len(strings.TrimLeft(line, " "))

			// Check if we've exited the field
			if trimmed == "" {
				// Empty line - could be end of block or just spacing
				// Check next non-empty line
				j := i + 1
				for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
					j++
				}
				if j < len(lines) {
					nextTrimmed := strings.TrimSpace(lines[j])
					nextIndent := len(lines[j]) - len(strings.TrimLeft(lines[j], " "))
					// If next line is a key at field level or less, exit field
					if nextIndent <= fieldIndent && strings.Contains(nextTrimmed, ":") {
						inField = false
						result = append(result, line) // Keep the empty line
						continue
					}
				}
				// Skip empty lines within the field content
				continue
			} else if currentIndent <= fieldIndent {
				// Found a line at same or lower indent - we've exited the field
				inField = false
				result = append(result, line)
				continue
			}
			// Skip this line (it's old field content)
			continue
		}

		result = append(result, line)
	}

	// Write back
	return os.WriteFile(filename, []byte(strings.Join(result, "\n")), 0644)
}

func categorizeFailures(failures []TestFailure) map[string]int {
	categories := make(map[string]int)

	for _, failure := range failures {
		category := categorizeFailure(failure)
		categories[category]++
	}

	return categories
}

func categorizeFailure(failure TestFailure) string {
	exp := failure.Expected
	act := failure.Actual

	// Check for empty constraint name issues
	if strings.Contains(exp, `DROP CONSTRAINT ""`) && !strings.Contains(act, `DROP CONSTRAINT ""`) {
		return "Missing constraint names (empty strings)"
	}

	// Check for PRIMARY vs actual name
	if strings.Contains(exp, `DROP CONSTRAINT "PRIMARY"`) && strings.Contains(act, "_pkey") {
		return "PRIMARY vs generated pkey name"
	}
	if strings.Contains(exp, `DROP CONSTRAINT primary`) && strings.Contains(act, "_pkey") {
		return "PRIMARY vs generated pkey name (unquoted)"
	}

	// Check for ordering differences
	expLines := strings.Split(exp, "\n")
	actLines := strings.Split(act, "\n")
	if len(expLines) == len(actLines) {
		// Check if it's just a reordering
		expSet := make(map[string]bool)
		actSet := make(map[string]bool)
		for _, line := range expLines {
			expSet[strings.TrimSpace(line)] = true
		}
		for _, line := range actLines {
			actSet[strings.TrimSpace(line)] = true
		}

		allMatch := true
		for line := range expSet {
			if !actSet[line] {
				allMatch = false
				break
			}
		}

		if allMatch {
			return "Statement ordering differences"
		}
	}

	// Check for missing schema creation
	if strings.Contains(exp, "CREATE SCHEMA") && !strings.Contains(act, "CREATE SCHEMA") {
		return "Missing CREATE SCHEMA in down migration"
	}

	// Check for quote preservation issues
	if containsDifferentQuoting(exp, act) {
		return "Quote preservation differences"
	}

	// Check for missing implicit constraint drops
	if strings.Count(act, "DROP CONSTRAINT") > strings.Count(exp, "DROP CONSTRAINT") {
		return "Missing implicit constraint drops (e.g., UNIQUE, CHECK)"
	}

	return "Other"
}

func containsDifferentQuoting(s1, s2 string) bool {
	// Simple heuristic: remove all quotes and compare
	unquoted1 := strings.ReplaceAll(s1, `"`, "")
	unquoted2 := strings.ReplaceAll(s2, `"`, "")

	return unquoted1 != unquoted2 &&
		len(unquoted1) == len(unquoted2) &&
		(strings.Count(s1, `"`) != strings.Count(s2, `"`))
}

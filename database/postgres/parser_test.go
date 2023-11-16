package postgres

import (
	"os"
	"testing"

	"github.com/sqldef/sqldef/database"
	"github.com/sqldef/sqldef/parser"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
)

func TestParse(t *testing.T) {
	tests, err := readTests("tests.yml")
	if err != nil {
		t.Fatal(err)
	}

	genericParser := database.NewParser(parser.ParserModePostgres)
	postgresParser := NewParser()
	postgresParser.testing = true
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			psqlResult, err := postgresParser.Parse(test.SQL)
			if err != nil {
				t.Fatal(err)
			}

			if !test.CompareWithGenericParser {
				return
			}

			genericResult, err := genericParser.Parse(test.SQL)
			if err != nil {
				t.Fatal(err)
			}

			assert.Equal(t, genericResult, psqlResult)
		})
	}
}

type TestCase struct {
	SQL                      string
	CompareWithGenericParser bool `yaml:"compare_with_generic_parser"`
}

func readTests(file string) (map[string]TestCase, error) {
	buf, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	var tests map[string]TestCase
	err = yaml.UnmarshalStrict(buf, &tests)
	if err != nil {
		return nil, err
	}

	return tests, nil
}

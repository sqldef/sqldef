package mssql

import (
	"bytes"
	"os"
	"testing"

	"github.com/goccy/go-yaml"
)

func TestParse(t *testing.T) {
	tests, err := readTests("tests.yml")
	if err != nil {
		t.Fatal(err)
	}

	sqlParser := NewParser()
	for name, sql := range tests {
		t.Run(name, func(t *testing.T) {
			_, err = sqlParser.Parse(sql)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func readTests(file string) (map[string]string, error) {
	buf, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	var tests map[string]string
	dec := yaml.NewDecoder(bytes.NewReader(buf), yaml.DisallowUnknownField())
	err = dec.Decode(&tests)
	if err != nil {
		return nil, err
	}

	return tests, nil
}

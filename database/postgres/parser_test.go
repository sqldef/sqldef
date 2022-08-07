package postgres

import (
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"testing"
)

func TestParse(t *testing.T) {
	tests, err := readTests("tests.yml")
	if err != nil {
		t.Fatal(err)
	}

	sqlParser := NewParser()
	for name, sql := range tests {
		t.Run(name, func(t *testing.T) {
			_, err = sqlParser.parseStmts(sql)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func readTests(file string) (map[string]string, error) {
	buf, err := ioutil.ReadFile(file)
	if err != nil {
		return nil, err
	}

	var tests map[string]string
	err = yaml.UnmarshalStrict(buf, &tests)
	if err != nil {
		return nil, err
	}

	return tests, nil
}
